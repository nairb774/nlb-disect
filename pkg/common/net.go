package common

import (
	"net"
	"time"
)

func Dialer() *net.Dialer {
	return &net.Dialer{
		Timeout: time.Second,
	}
}

func ListenConfig() *net.ListenConfig {
	return &net.ListenConfig{}
}
