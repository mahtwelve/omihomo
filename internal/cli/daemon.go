package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/readeem/omihomo/internal/client"
	"github.com/readeem/omihomo/internal/core"
	"github.com/readeem/omihomo/internal/core/fakecore"
	"github.com/readeem/omihomo/internal/daemon"
	"github.com/readeem/omihomo/internal/owner"
	"github.com/readeem/omihomo/internal/systemd"
)

// runDaemon serves until SIGTERM or SIGINT. Under systemd it takes the
// socket from socket activation and reports readiness; started by hand it
// binds the socket itself.
func runDaemon(ctx context.Context, args []string, env Env) int {
	flags := flag.NewFlagSet("omihomo daemon", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	socket := flags.String("socket", client.DefaultSocket, "socket to bind when not socket-activated")
	ownerFile := flags.String("owner-file", owner.DefaultPath, "file recording the owner's uid")
	coreName := flags.String("core", "", "proxy core to run: fake")
	if err := flags.Parse(args); err != nil {
		return ExitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(env.Stderr, "omihomo daemon takes no arguments")
		return ExitUsage
	}
	log := slog.New(slog.NewTextHandler(env.Stderr, nil))

	var c core.Core
	switch *coreName {
	case "fake":
		c = fakecore.New()
		log.Warn("running the fake core: nothing is proxied")
	case "":
		fmt.Fprintln(env.Stderr, "omihomo daemon: no proxy core is built in yet; pass --core fake")
		return ExitUsage
	default:
		fmt.Fprintf(env.Stderr, "omihomo daemon: unknown core %q\n", *coreName)
		return ExitUsage
	}

	listeners, err := systemd.Listeners()
	if err != nil {
		log.Error("socket activation", "err", err)
		return ExitFailure
	}
	if len(listeners) == 0 {
		l, err := listen(*socket, *ownerFile)
		if err != nil {
			log.Error("bind the socket", "path", *socket, "err", err)
			return ExitFailure
		}
		listeners = append(listeners, l)
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	d := daemon.New(daemon.Options{Core: c, OwnerFile: *ownerFile, Version: Version, Logger: log})
	if err := systemd.Notify("READY=1"); err != nil {
		log.Warn("notify systemd", "err", err)
	}
	log.Info("serving", "version", Version, "socket", listeners[0].Addr().String())
	err = d.Run(ctx, listeners...)
	systemd.Notify("STOPPING=1")
	if err != nil {
		log.Error("serve", "err", err)
		return ExitFailure
	}
	return ExitOK
}

// listen binds path as a socket only its owner can open. Under systemd the
// socket unit does this instead, which is what lets a sandboxed daemon
// without CAP_CHOWN hand the socket to the owner.
//
// The socket's directory must belong to the daemon's account and be
// writable by nobody else; otherwise another account could swap the path
// between the bind and the chown and have root chown something else.
func listen(path, ownerFile string) (net.Listener, error) {
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(st.Uid) != os.Geteuid() || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s must be a directory owned by uid %d and writable by nobody else", dir, os.Geteuid())
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode().Type() != fs.ModeSocket {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if conn, err := net.Dial("unix", path); err == nil {
			conn.Close()
			return nil, fmt.Errorf("another daemon is serving %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	old := syscall.Umask(0o177)
	l, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 {
		uid, err := owner.Load(ownerFile)
		if err == nil {
			err = os.Lchown(path, uid, -1)
		}
		if err != nil {
			l.Close()
			return nil, fmt.Errorf("hand the socket to the owner: %w", err)
		}
	}
	return l, nil
}
