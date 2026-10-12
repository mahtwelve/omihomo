package systemd

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestActivated(t *testing.T) {
	for _, c := range []struct {
		pid, fds string
		want     int
		err      bool
	}{
		{"", "", 0, false},
		{"42", "1", 1, false},
		{"43", "1", 0, false}, // meant for another process
		{"42", "x", 0, true},
		{"42", "-1", 0, true},
	} {
		n, err := activated(c.pid, c.fds, 42)
		if n != c.want || (err != nil) != c.err {
			t.Errorf("activated(%q, %q) = %d, %v", c.pid, c.fds, n, err)
		}
	}
}

func TestListenersWithoutActivation(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")
	if ls, err := Listeners(); ls != nil || err != nil {
		t.Fatalf("Listeners() = %v, %v", ls, err)
	}
}

func TestNotify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	t.Setenv("NOTIFY_SOCKET", path)
	if err := Notify("READY=1"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "READY=1" {
		t.Fatalf("got %q, %v", buf[:n], err)
	}

	t.Setenv("NOTIFY_SOCKET", "")
	if err := Notify("READY=1"); err != nil {
		t.Fatalf("outside systemd: %v", err)
	}
}
