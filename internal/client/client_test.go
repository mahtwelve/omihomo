package client_test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/readeem/omihomo/internal/client"
)

func TestDialObservesCancellationDuringTheGreeting(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			accepted <- conn // and never greet
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		c, err := client.Dial(ctx, socket)
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	conn := <-accepted
	defer conn.Close()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dial = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dial still blocked after cancellation")
	}
}
