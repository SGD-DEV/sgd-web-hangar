package services

import (
	"fmt"
	"net"
	"time"
)

// WaitPortFree polls until nothing accepts connections on 127.0.0.1:port or
// the timeout passes. Returns true when the port is free.
func WaitPortFree(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err != nil {
			return true
		}
		c.Close()
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}
