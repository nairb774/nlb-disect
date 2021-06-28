package common

import (
	"flag"
	"log"
	"os"
)

func mustHostname() string {
	name, err := os.Hostname()
	if err != nil {
		log.Panic(err)
	}
	return name
}

var (
	name = flag.String("name", mustHostname(), "")
)

func Name() string { return *name }
