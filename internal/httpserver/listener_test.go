package httpserver

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestListenAndServeTLSReadyDoesNotAdvertiseFailedBind(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	server := newSecurityTestServer(t)
	server.httpServer.Addr = occupied.Addr().String()
	server.maxConnections = 1
	called := false
	if err := server.ListenAndServeTLSReady(context.Background(), func() { called = true }); err == nil {
		t.Fatal("ListenAndServeTLSReady() unexpectedly succeeded on an occupied address")
	}
	if called {
		t.Fatal("readiness callback ran after a failed bind")
	}
}

func TestListenAndServeTLSReadyAdvertisesAfterSuccessfulBind(t *testing.T) {
	server := newSecurityTestServer(t)
	server.httpServer.Addr = "127.0.0.1:0"
	server.maxConnections = 1
	ready := make(chan struct{})
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.ListenAndServeTLSReady(context.Background(), func() {
			close(ready)
		})
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("successful bind did not trigger readiness callback")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ListenAndServeTLSReady() = %v after close", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after close")
	}
}
