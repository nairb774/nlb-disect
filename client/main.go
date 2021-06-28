package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/nairb774/nlb-disect/pkg/common"
	"github.com/nairb774/nlb-disect/pkg/proto"
)

var (
	endpoint       = flag.String("endpoint", "", "Location to probe connections")
	longLivedLimit = flag.Int("long_lived", 5, "Number of unique long lived connections to maintain per remote")
)

var state = struct {
	mu sync.Mutex

	longLived map[string]int

	lastConnectedAt map[string]time.Time
}{
	longLived:       make(map[string]int),
	lastConnectedAt: make(map[string]time.Time),
}

func shouldRetain(started time.Time, remoteName string) bool {
	state.mu.Lock()
	defer state.mu.Unlock()

	if prev := state.lastConnectedAt[remoteName]; prev.Before(started) {
		state.lastConnectedAt[remoteName] = started
	}

	if longLived := state.longLived[remoteName]; longLived < *longLivedLimit {
		state.longLived[remoteName]++
		return true
	}
	return false
}

func releaseLongLived(remoteName string) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.longLived[remoteName]--
	if state.longLived[remoteName] == 0 {
		log.Printf("%v last long lived closed", remoteName)
		delete(state.longLived, remoteName)
	}
}

func cleanLastConnected(threshold time.Duration) {
	start := time.Now()
	state.mu.Lock()
	defer state.mu.Unlock()

	for k, v := range state.lastConnectedAt {
		if delay := start.Sub(v); delay >= threshold {
			log.Printf("S:%v last seen at %v (%v ago)", k, v, delay)
			delete(state.lastConnectedAt, k)
		}
	}
}

func attemptConnection(ctx context.Context, addr *net.TCPAddr) {
	start := time.Now()
	c, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		log.Printf("%v failed to dial: %v", addr, err)
		return
	}
	defer c.Close()

	const initialRemoteName = "???"
	remoteName := initialRemoteName

	id := func() string {
		return fmt.Sprintf("S:%s@%v C:%s@%v", remoteName, c.RemoteAddr(), common.Name(), c.LocalAddr())
	}

	r := json.NewDecoder(c)
	w := json.NewEncoder(c)

	recv := func() (proto.Message, error) {
		if err := c.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			log.Panicf("%v unable to set read deadline: %v", id(), err)
		}

		var in proto.Message
		if err := r.Decode(&in); err != nil {
			return in, err
		}
		if remoteName == initialRemoteName {
			remoteName = in.Name
		}
		return in, nil
	}

	send := func(msg proto.Message) error {
		if err := c.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			log.Panicf("%v unable to set write deadline: %v", id(), err)
		}

		return w.Encode(msg)
	}

	if err := send(proto.Message{
		Name: common.Name(),
	}); err != nil {
		log.Printf("%v failed initial send: %v", id(), err)
		return
	}

	if _, err := recv(); err != nil {
		log.Printf("%v failed initial read: %v", id(), err)
		return
	}

	if ll := fmt.Sprintf("%v@%v", remoteName, c.RemoteAddr()); shouldRetain(start, ll) {
		defer releaseLongLived(ll)

		t := time.NewTicker(time.Second)
		defer t.Stop()
		for ctx.Err() == nil {
			if err := send(proto.Message{
				Name: common.Name(),
			}); err != nil {
				log.Printf("%v error in long lived send: %v", id(), err)
				return
			}

			if _, err := recv(); err != nil {
				log.Printf("%v error in long lived recv: %v", id(), err)
				return
			}

			select {
			case <-t.C:
			case <-ctx.Done():
			}
		}
	}

	if err := send(proto.Message{
		Name:  common.Name(),
		Close: true,
	}); err != nil {
		log.Printf("%v failed to send close request: %v", id(), err)
	}

	if err := c.CloseWrite(); err != nil {
		log.Printf("%v while CloseWrite: %v", id(), err)
	}

	if v, err := recv(); err != nil {
		log.Printf("%v error in read close: %v", id(), err)
	} else if !v.Close {
		log.Printf("%v ping desync: %#v", id(), v)
	}
}

func main() {
	ctx := context.Background()
	flag.Parse()

	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for ctx.Err() == nil {
			cleanLastConnected(30 * time.Second)

			select {
			case <-t.C:
			case <-ctx.Done():
			}
		}
	}()

	host, portString, err := net.SplitHostPort(*endpoint)
	if err != nil {
		log.Panic(err)
	}

	port, err := strconv.Atoi(portString)
	if err != nil {
		log.Panic(err)
	}

	t := time.NewTicker(time.Second)
	defer t.Stop()
	for ctx.Err() == nil {
		addrs, err := net.LookupIP(host)
		if err != nil {
			log.Printf("Error while resolving %v: %T %v", host, err, err)
		}

		if len(addrs) == 0 {
			log.Printf("%v resolved no addresses", host)
		}

		for _, addr := range addrs {
			addr := &net.TCPAddr{
				IP:   addr,
				Port: port,
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				attemptConnection(ctx, addr)
			}()
		}

		select {
		case <-t.C:
		case <-ctx.Done():
		}
	}
}
