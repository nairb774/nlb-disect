package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nairb774/nlb-disect/pkg/common"
	"github.com/nairb774/nlb-disect/pkg/proto"
)

var (
	httpServerAddr = flag.String("http_server_addr", ":8080", "Port to run HTTP server on.")
	tcpServerAddr  = flag.String("tcp_server_addr", ":9090", "Address to run tcp server on")
)

var state = struct {
	mu sync.Mutex

	termAt time.Time

	lastHealthCheckAt time.Time
}{}

func handleConn(c *net.TCPConn) {
	defer c.Close()

	const initialRemoteName = "???"
	remoteName := initialRemoteName

	id := func() string {
		return fmt.Sprintf("S:%s@%v C:%s@%v", common.Name(), c.LocalAddr(), remoteName, c.RemoteAddr())
	}

	r := json.NewDecoder(c)
	w := json.NewEncoder(c)

	for i := 0; true; i++ {
		if err := c.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			log.Panicf("%v unable to set read deadline: %v", id(), err)
		}

		var in proto.Message
		if err := r.Decode(&in); err != nil {
			log.Printf("%v closing. Unexpected read error: %v", id(), err)
			return
		}

		if remoteName == initialRemoteName {
			remoteName = in.Name
		}

		if err := c.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			log.Panicf("%v unable to set write deadline: %v", id(), err)
		}

		if err := w.Encode(proto.Message{
			Name:  common.Name(),
			Close: in.Close,
		}); err != nil {
			log.Printf("%v closing. Unexpected write error: %v", id(), err)
			return
		}

		if in.Close {
			break
		}
	}
}

func main() {
	ctx := context.Background()
	flag.Parse()

	sigTerm := make(chan os.Signal, 1)
	go func() {
		defer signal.Stop(sigTerm)
		sig := <-sigTerm
		now := time.Now()
		state.mu.Lock()
		defer state.mu.Unlock()
		state.termAt = now
		log.Printf("Received signal %v at %v", sig, now)
	}()
	signal.Notify(sigTerm, os.Interrupt, syscall.SIGTERM)

	s := &http.Server{
		Addr: *httpServerAddr,
		// TODO: ConnState: func(c net.Conn, state http.ConnState) {},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			log.Printf("HTTP %v from %v", r.URL, r.RemoteAddr)

			func() {
				state.mu.Lock()
				defer state.mu.Unlock()
				if now.After(state.lastHealthCheckAt) {
					state.lastHealthCheckAt = now
				}
			}()

			w.WriteHeader(http.StatusOK)
		}),
	}

	go func() {
		if err := s.ListenAndServe(); err != http.ErrServerClosed {
			log.Panic(err)
		}
	}()

	l, err := common.ListenConfig().Listen(ctx, "tcp", *tcpServerAddr)
	if err != nil {
		log.Panic(err)
	}

	tcpListener := l.(*net.TCPListener)
	for {
		c, err := tcpListener.AcceptTCP()
		if err != nil {
			log.Panic(err)
		}
		go handleConn(c)
	}
}
