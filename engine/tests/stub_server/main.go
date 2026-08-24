package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	sockPath := "/tmp/aivision_app_test.sock"
	os.Remove(sockPath)

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		fmt.Printf("Failed to listen: %v\n", err)
		return
	}
	defer l.Close()
	defer os.Remove(sockPath)

	fmt.Println("Test Go Stub Server listening on", sockPath)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
}
