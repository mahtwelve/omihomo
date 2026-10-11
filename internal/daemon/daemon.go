// Package daemon serves the protocol to the owner over a unix socket and
// drives a core.Core on its behalf.
package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/readeem/omihomo/internal/core"
	"github.com/readeem/omihomo/internal/owner"
	"github.com/readeem/omihomo/internal/peercred"
	"github.com/readeem/omihomo/internal/protocol"
)

// Options configures a Daemon. Core is required; the rest have defaults.
type Options struct {
	Core core.Core
	// OwnerFile records the one uid the socket serves; see package owner.
	// It is read on every connection, so claiming ownership needs no restart.
	OwnerFile string
	Version   string
	Logger    *slog.Logger
	// Tick is how often the traffic and connections streams push.
	Tick time.Duration
	// EgressURL answers with a Cloudflare-style trace: "ip=<addr>" lines.
	EgressURL string
	// TailscalePresent reports whether tailscaled is installed.
	TailscalePresent func() bool
	MaxClients       int
}

const (
	defaultTick       = time.Second
	defaultEgressURL  = "https://cloudflare.com/cdn-cgi/trace"
	defaultMaxClients = 16
	// Calls one client may have in flight at once.
	maxInFlight = 8
	// Messages queued for one client before it counts as stuck.
	sendQueue    = 256
	writeTimeout = 5 * time.Second
	callTimeout  = 2 * time.Minute
)

// Daemon is the server. Create one with New and start it with Run.
type Daemon struct {
	opts Options
	log  *slog.Logger

	// clients tracks every accepted connection until it is gone.
	clients sync.WaitGroup

	// opMu serializes the calls that change what the core runs.
	opMu sync.Mutex

	mu        sync.Mutex
	state     string
	detail    string
	startedAt int64
	settings  settings
	sessions  map[*session]struct{}

	// pubMu orders pushes, so a client never sees an older value of a state
	// stream after a newer one.
	pubMu      sync.Mutex
	lastPushed map[string][]byte
}

// New returns a daemon with the proxy stopped.
func New(opts Options) *Daemon {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Tick <= 0 {
		opts.Tick = defaultTick
	}
	if opts.EgressURL == "" {
		opts.EgressURL = defaultEgressURL
	}
	if opts.TailscalePresent == nil {
		opts.TailscalePresent = func() bool {
			_, err := exec.LookPath("tailscaled")
			return err == nil
		}
	}
	if opts.MaxClients <= 0 {
		opts.MaxClients = defaultMaxClients
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	return &Daemon{
		opts:       opts,
		log:        opts.Logger,
		state:      protocol.StateStopped,
		settings:   defaultSettings(),
		sessions:   map[*session]struct{}{},
		lastPushed: map[string][]byte{},
	}
}

// Run serves listeners until ctx is done, then disconnects every client and
// stops the core. It closes the listeners.
func (d *Daemon) Run(ctx context.Context, listeners ...net.Listener) error {
	if len(listeners) == 0 {
		return errors.New("no socket to serve")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, len(listeners))
	for _, l := range listeners {
		wg.Go(func() {
			if err := d.accept(ctx, l); err != nil {
				errs <- err
				cancel()
			}
		})
	}
	wg.Go(func() { d.tick(ctx) })

	<-ctx.Done()
	for _, l := range listeners {
		l.Close()
	}
	wg.Wait()
	d.clients.Wait()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := d.opts.Core.Stop(stopCtx); err != nil {
		d.log.Error("stop the core on shutdown", "err", err)
	}
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

func (d *Daemon) accept(ctx context.Context, l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		d.clients.Go(func() { d.admit(ctx, conn) })
	}
}

// admit greets an owner and refuses anyone else.
func (d *Daemon) admit(ctx context.Context, conn net.Conn) {
	refuse := func(code, message string) {
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		if b, err := protocol.Line(protocol.Refused{Type: protocol.TypeRefused,
			Error: &protocol.Error{Code: code, Message: message}}); err == nil {
			conn.Write(b)
		}
		conn.Close()
	}

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		refuse(protocol.CodeForbidden, "only unix socket peers are served")
		return
	}
	uid, err := peercred.UID(unixConn)
	if err != nil {
		d.log.Warn("refused a client whose credentials could not be read", "err", err)
		refuse(protocol.CodeForbidden, "could not identify the connecting account")
		return
	}
	ownerUID, err := owner.Load(d.opts.OwnerFile)
	if err != nil {
		d.log.Warn("refused a client: no usable owner", "uid", uid, "err", err)
		if errors.Is(err, owner.ErrUnclaimed) {
			refuse(protocol.CodeForbidden, err.Error())
		} else {
			refuse(protocol.CodeForbidden, "the owner record is unusable; reinstall the core")
		}
		return
	}
	if uid != ownerUID {
		d.log.Warn("refused a client that is not the owner", "uid", uid)
		refuse(protocol.CodeForbidden, "this account does not own omihomo")
		return
	}

	s := newSession(d, conn)
	d.mu.Lock()
	full := len(d.sessions) >= d.opts.MaxClients
	if !full {
		d.sessions[s] = struct{}{}
	}
	d.mu.Unlock()
	if full {
		refuse(protocol.CodeBusy, "too many clients")
		return
	}
	s.send(protocol.Hello{Type: protocol.TypeHello, Protocol: protocol.Version, Version: d.opts.Version})
	s.serve(ctx)

	d.mu.Lock()
	delete(d.sessions, s)
	d.mu.Unlock()
}

// tick pushes the periodic streams while anyone is listening.
func (d *Daemon) tick(ctx context.Context) {
	t := time.NewTicker(d.opts.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !d.running() {
			continue
		}
		if d.listening(protocol.StreamTraffic) {
			if v, err := d.opts.Core.Traffic(ctx); err == nil {
				d.broadcast(protocol.StreamTraffic, v)
			}
		}
		if d.listening(protocol.StreamConnections) {
			if v, err := d.opts.Core.Connections(ctx); err == nil {
				d.broadcast(protocol.StreamConnections, v)
			}
		}
	}
}

func (d *Daemon) running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state == protocol.StateOn || d.state == protocol.StateDegraded
}

func (d *Daemon) listening(stream string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for s := range d.sessions {
		if s.wants(stream) {
			return true
		}
	}
	return false
}

func (d *Daemon) subscribers(stream string) []*session {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []*session
	for s := range d.sessions {
		if s.wants(stream) {
			out = append(out, s)
		}
	}
	return out
}

// broadcast pushes v on stream to every subscriber.
func (d *Daemon) broadcast(stream string, v any) {
	b, err := protocol.Line(protocol.Event{Type: protocol.TypeEvent, Stream: stream, Data: v})
	if err != nil {
		d.log.Error("encode event", "stream", stream, "err", err)
		return
	}
	for _, s := range d.subscribers(stream) {
		s.sendLine(b)
	}
}
