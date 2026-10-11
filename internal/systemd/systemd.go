// Package systemd is the little of systemd's service protocol the daemon
// needs: socket activation and readiness notification. Both are no-ops when
// the daemon was not started by systemd.
package systemd

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
)

const listenFdsStart = 3

// Listeners returns the sockets systemd passed to this process, or nil when
// it passed none. It unsets the environment so children do not inherit it.
func Listeners() ([]net.Listener, error) {
	defer os.Unsetenv("LISTEN_PID")
	defer os.Unsetenv("LISTEN_FDS")
	defer os.Unsetenv("LISTEN_FDNAMES")
	n, err := activated(os.Getenv("LISTEN_PID"), os.Getenv("LISTEN_FDS"), os.Getpid())
	if err != nil || n == 0 {
		return nil, err
	}
	out := make([]net.Listener, 0, n)
	for fd := listenFdsStart; fd < listenFdsStart+n; fd++ {
		syscall.CloseOnExec(fd)
		f := os.NewFile(uintptr(fd), "LISTEN_FD_"+strconv.Itoa(fd))
		l, err := net.FileListener(f)
		f.Close()
		if err != nil {
			for _, l := range out {
				l.Close()
			}
			return nil, fmt.Errorf("socket activation fd %d: %w", fd, err)
		}
		out = append(out, l)
	}
	return out, nil
}

// activated is how many fds systemd passed, given its environment.
func activated(pid, fds string, self int) (int, error) {
	if pid == "" || fds == "" {
		return 0, nil
	}
	p, err := strconv.Atoi(pid)
	if err != nil || p != self {
		return 0, nil
	}
	n, err := strconv.Atoi(fds)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid LISTEN_FDS %q", fds)
	}
	return n, nil
}

// Notify sends state, such as "READY=1" or "STOPPING=1", to the service
// manager. It does nothing outside a Type=notify unit.
func Notify(state string) error {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return nil
	}
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}
