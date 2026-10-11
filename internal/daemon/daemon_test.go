package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/readeem/omihomo/internal/client"
	"github.com/readeem/omihomo/internal/core/fakecore"
	"github.com/readeem/omihomo/internal/daemon"
	"github.com/readeem/omihomo/internal/protocol"
)

type harness struct {
	socket string
	core   *fakecore.Core
	done   chan error
	cancel context.CancelFunc
}

// serve runs a daemon on a fresh socket with the given owner uid, or no
// owner file at all when ownerUID is negative.
func serve(t *testing.T, ownerUID int, tweak func(*daemon.Options)) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("", "omihomo-test-") // short: socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ownerFile := filepath.Join(dir, "owner")
	if ownerUID >= 0 {
		if err := os.WriteFile(ownerFile, []byte(fmt.Sprintf("%d\n", ownerUID)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h := &harness{socket: filepath.Join(dir, "s"), core: fakecore.New(), done: make(chan error, 1)}
	l, err := net.Listen("unix", h.socket)
	if err != nil {
		t.Fatal(err)
	}
	opts := daemon.Options{
		Core:             h.core,
		OwnerFile:        ownerFile,
		Version:          "test",
		Tick:             10 * time.Millisecond,
		TailscalePresent: func() bool { return true },
		Logger:           discardLogger(),
	}
	if tweak != nil {
		tweak(&opts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	d := daemon.New(opts)
	go func() { h.done <- d.Run(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not shut down")
		}
	})
	return h
}

func (h *harness) dial(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.Dial(context.Background(), h.socket)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func call[T any](t *testing.T, c *client.Client, op string, args any) T {
	t.Helper()
	var out T
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Call(ctx, op, args, &out); err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return out
}

func callErr(t *testing.T, c *client.Client, op string, args any) *protocol.Error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Call(ctx, op, args, nil)
	var perr *protocol.Error
	if !errors.As(err, &perr) {
		t.Fatalf("%s: want a protocol error, got %v", op, err)
	}
	return perr
}

func next(t *testing.T, c *client.Client, stream string) json.RawMessage {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				t.Fatalf("connection closed waiting for %s", stream)
			}
			if ev.Stream == stream {
				return ev.Data
			}
		case <-deadline:
			t.Fatalf("no %s event", stream)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestOwnerIsGreeted(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	if c.Hello.Protocol != protocol.Version || c.Hello.Version != "test" {
		t.Fatalf("hello = %+v", c.Hello)
	}
	if st := call[protocol.Status](t, c, protocol.OpStatus, nil); st.State != protocol.StateStopped {
		t.Fatalf("state = %q", st.State)
	}
}

func TestAnyoneElseIsRefused(t *testing.T) {
	cases := map[string]func(t *testing.T) *harness{
		"another uid owns it": func(t *testing.T) *harness { return serve(t, os.Getuid()+1, nil) },
		"nobody claimed it":   func(t *testing.T) *harness { return serve(t, -1, nil) },
		"the owner file is writable by others": func(t *testing.T) *harness {
			h := serve(t, os.Getuid(), nil)
			os.Chmod(filepath.Join(filepath.Dir(h.socket), "owner"), 0o666)
			return h
		},
		"the owner file is a symlink": func(t *testing.T) *harness {
			h := serve(t, -1, nil)
			dir := filepath.Dir(h.socket)
			os.WriteFile(filepath.Join(dir, "real"), []byte(fmt.Sprint(os.Getuid())), 0o600)
			os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "owner"))
			return h
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			h := setup(t)
			_, err := client.Dial(context.Background(), h.socket)
			var perr *protocol.Error
			if !errors.As(err, &perr) || perr.Code != protocol.CodeForbidden {
				t.Fatalf("dial = %v, want forbidden", err)
			}

			// The refusal is the only line, and nothing a refused peer sends
			// gets an answer.
			conn, err := net.Dial("unix", h.socket)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			fmt.Fprintln(conn, `{"id":1,"op":"start"}`)
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			all, _ := io.ReadAll(conn)
			lines := strings.Split(strings.TrimSpace(string(all)), "\n")
			if len(lines) != 1 || !strings.Contains(lines[0], `"type":"refused"`) {
				t.Fatalf("refused peer read %q", all)
			}
			if _, running := h.core.Config(); running {
				t.Fatal("a refused peer started the core")
			}
		})
	}
}

func TestStartStop(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	if err := callErr(t, c, protocol.OpGroups, nil); err.Code != protocol.CodeNotRunning {
		t.Fatalf("groups while stopped = %v", err)
	}
	st := call[protocol.Status](t, c, protocol.OpStart, nil)
	if st.State != protocol.StateOn || st.StartedAt == 0 {
		t.Fatalf("after start: %+v", st)
	}
	if again := call[protocol.Status](t, c, protocol.OpStart, nil); again.StartedAt != st.StartedAt {
		t.Fatal("a second start restarted the core")
	}
	st = call[protocol.Status](t, c, protocol.OpStop, nil)
	if st.State != protocol.StateStopped || st.StartedAt != 0 {
		t.Fatalf("after stop: %+v", st)
	}
	if _, running := h.core.Config(); running {
		t.Fatal("core still running")
	}
}

func TestFailedStartReportsWhy(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	h.core.FailApply(errors.New("tun device busy"))
	c := h.dial(t)
	if err := callErr(t, c, protocol.OpStart, nil); err.Code != protocol.CodeCore {
		t.Fatalf("start = %v", err)
	}
	st := call[protocol.Status](t, c, protocol.OpStatus, nil)
	if st.State != protocol.StateStopped || st.Detail != "tun device busy" {
		t.Fatalf("status = %+v", st)
	}
}

func TestSettings(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)

	st := call[protocol.Status](t, c, protocol.OpSetSettings, protocol.SettingsArgs{Mode: ptr("global"), TUN: ptr(true)})
	if st.Mode != "global" || !st.TUN {
		t.Fatalf("stopped settings: %+v", st)
	}
	call[protocol.Status](t, c, protocol.OpStart, nil)
	if cfg, _ := h.core.Config(); cfg.Mode != "global" || !cfg.TUN {
		t.Fatalf("core started on %+v", cfg)
	}

	call[protocol.Status](t, c, protocol.OpSetSettings, protocol.SettingsArgs{TUNRedirect: ptr(true), PrimaryGroup: ptr("Proxy")})
	if cfg, _ := h.core.Config(); !cfg.TUNRedirect || cfg.Mode != "global" {
		t.Fatalf("live change applied %+v", cfg)
	}

	for name, args := range map[string]protocol.SettingsArgs{
		"mode":  {Mode: ptr("tunnel")},
		"group": {PrimaryGroup: ptr("bad\nname")},
	} {
		if err := callErr(t, c, protocol.OpSetSettings, args); err.Code != protocol.CodeInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := callErr(t, c, protocol.OpSetSettings, protocol.SettingsArgs{PrimaryGroup: ptr("Auto")}); err.Code != protocol.CodeNotFound {
		t.Errorf("a group that cannot be selected became primary: %v", err)
	}

	// A change the core refuses changes nothing.
	h.core.FailApply(errors.New("bad config"))
	if err := callErr(t, c, protocol.OpSetSettings, protocol.SettingsArgs{Mode: ptr("direct")}); err.Code != protocol.CodeCore {
		t.Fatalf("refused apply = %v", err)
	}
	if st := call[protocol.Status](t, c, protocol.OpStatus, nil); st.Mode != "global" {
		t.Fatalf("mode after a refused change = %q", st.Mode)
	}
}

func TestTailscaleNeedsTailscaled(t *testing.T) {
	h := serve(t, os.Getuid(), func(o *daemon.Options) { o.TailscalePresent = func() bool { return false } })
	c := h.dial(t)
	if err := callErr(t, c, protocol.OpSetSettings, protocol.SettingsArgs{Tailscale: ptr(true)}); err.Code != protocol.CodeConflict {
		t.Fatalf("tailscale on = %v", err)
	}
	call[protocol.Status](t, c, protocol.OpSetSettings, protocol.SettingsArgs{Tailscale: ptr(false)})
}

func TestGroups(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	call[protocol.Status](t, c, protocol.OpStart, nil)

	groups := call[protocol.GroupList](t, c, protocol.OpGroups, nil)
	if len(groups.Groups) != 2 || groups.Groups[0].Now != "Tokyo" {
		t.Fatalf("groups = %+v", groups)
	}
	g := call[protocol.Group](t, c, protocol.OpSelect, protocol.SelectArgs{Group: "Proxy", Proxy: "Amsterdam"})
	if g.Now != "Amsterdam" {
		t.Fatalf("selected %q", g.Now)
	}
	for _, args := range []protocol.SelectArgs{{Group: "Proxy", Proxy: "Mars"}, {Group: "Auto", Proxy: "Tokyo"}} {
		if err := callErr(t, c, protocol.OpSelect, args); err.Code != protocol.CodeNotFound {
			t.Errorf("select %+v = %v", args, err)
		}
	}

	delays := call[protocol.DelayResult](t, c, protocol.OpDelayTest, protocol.DelayArgs{Group: "Auto"})
	if len(delays.Delays) != 3 || delays.Delays["Tokyo"] != 120 || delays.Delays["Frankfurt"] != 0 {
		t.Fatalf("delays = %+v", delays)
	}
	one := call[protocol.DelayResult](t, c, protocol.OpDelayTest, protocol.DelayArgs{Group: "Proxy", Proxies: []string{"DIRECT"}})
	if len(one.Delays) != 1 || one.Delays["DIRECT"] != 3 {
		t.Fatalf("single delay = %+v", one)
	}
}

func TestRulesAndConnections(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	if rules := call[protocol.RuleList](t, c, protocol.OpRules, nil); len(rules.Rules) != 0 {
		t.Fatalf("rules while stopped = %+v", rules)
	}
	call[protocol.Status](t, c, protocol.OpStart, nil)
	if rules := call[protocol.RuleList](t, c, protocol.OpRules, nil); len(rules.Rules) != 2 {
		t.Fatalf("rules = %+v", rules)
	}

	conns := call[protocol.Connections](t, c, protocol.OpConnections, nil)
	if len(conns.Connections) != 2 || conns.DownloadTotal != 48900 {
		t.Fatalf("connections = %+v", conns)
	}
	for _, bad := range []protocol.CloseArgs{{}, {IDs: []string{"c1"}, All: true}, {IDs: []string{""}}} {
		if err := callErr(t, c, protocol.OpCloseConnections, bad); err.Code != protocol.CodeInvalid {
			t.Errorf("close %+v = %v", bad, err)
		}
	}
	call[protocol.Empty](t, c, protocol.OpCloseConnections, protocol.CloseArgs{IDs: []string{"c1", "gone"}})
	if conns := call[protocol.Connections](t, c, protocol.OpConnections, nil); len(conns.Connections) != 1 {
		t.Fatalf("after closing c1: %+v", conns)
	}
	call[protocol.Empty](t, c, protocol.OpCloseConnections, protocol.CloseArgs{All: true})
	if conns := call[protocol.Connections](t, c, protocol.OpConnections, nil); len(conns.Connections) != 0 {
		t.Fatalf("after closing all: %+v", conns)
	}
}

func TestEgress(t *testing.T) {
	trace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "fl=1\nip=203.0.113.7\nloc=NL\n")
	}))
	defer trace.Close()
	h := serve(t, os.Getuid(), func(o *daemon.Options) { o.EgressURL = trace.URL })
	c := h.dial(t)
	if err := callErr(t, c, protocol.OpEgress, nil); err.Code != protocol.CodeNotRunning {
		t.Fatalf("egress while stopped = %v", err)
	}
	call[protocol.Status](t, c, protocol.OpStart, nil)
	eg := call[protocol.Egress](t, c, protocol.OpEgress, nil)
	if eg.IP != "203.0.113.7" || eg.Latency <= 0 {
		t.Fatalf("egress = %+v", eg)
	}
}

func TestUnsupportedOpsAreNamed(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	for _, op := range []string{protocol.OpSubscriptions, protocol.OpSubscriptionAdd, protocol.OpRuleAdd} {
		if err := callErr(t, c, op, nil); err.Code != protocol.CodeUnsupported {
			t.Errorf("%s = %v", op, err)
		}
	}
	if err := callErr(t, c, "reboot", nil); err.Code != protocol.CodeUnknownOp {
		t.Errorf("unknown op = %v", err)
	}
}

func TestStateStreams(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	watcher := h.dial(t)
	call[protocol.Empty](t, watcher, protocol.OpSubscribe, protocol.StreamsArgs{Streams: []string{"status", "groups"}})

	// The current value arrives with the subscription.
	var st protocol.Status
	json.Unmarshal(next(t, watcher, "status"), &st)
	if st.State != protocol.StateStopped {
		t.Fatalf("initial status = %+v", st)
	}
	next(t, watcher, "groups")

	// Changes another client makes reach the watcher.
	actor := h.dial(t)
	call[protocol.Status](t, actor, protocol.OpStart, nil)
	for st.State != protocol.StateOn {
		json.Unmarshal(next(t, watcher, "status"), &st)
	}
	call[protocol.Group](t, actor, protocol.OpSelect, protocol.SelectArgs{Group: "Proxy", Proxy: "DIRECT"})
	for {
		var groups protocol.GroupList
		json.Unmarshal(next(t, watcher, "groups"), &groups)
		if len(groups.Groups) > 0 && groups.Groups[0].Now == "DIRECT" {
			break
		}
	}

	call[protocol.Empty](t, watcher, protocol.OpUnsubscribe, protocol.StreamsArgs{Streams: []string{"status"}})
	if err := callErr(t, watcher, protocol.OpSubscribe, protocol.StreamsArgs{Streams: []string{"weather"}}); err.Code != protocol.CodeInvalid {
		t.Fatalf("unknown stream = %v", err)
	}
}

func TestPeriodicStreams(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	call[protocol.Status](t, c, protocol.OpStart, nil)
	call[protocol.Empty](t, c, protocol.OpSubscribe, protocol.StreamsArgs{Streams: []string{"traffic", "connections"}})
	var tr protocol.Traffic
	json.Unmarshal(next(t, c, "traffic"), &tr)
	if tr.Down != 65536 {
		t.Fatalf("traffic = %+v", tr)
	}
	var conns protocol.Connections
	json.Unmarshal(next(t, c, "connections"), &conns)
	if len(conns.Connections) != 2 {
		t.Fatalf("connections = %+v", conns)
	}
}

// raw talks to the daemon line by line, for what a well-behaved client
// never sends.
func raw(t *testing.T, h *harness) (net.Conn, *bufio.Scanner) {
	t.Helper()
	conn, err := net.Dial("unix", h.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	s := protocol.NewScanner(conn)
	if !s.Scan() || !strings.Contains(s.Text(), `"type":"hello"`) {
		t.Fatalf("no hello: %q", s.Text())
	}
	return conn, s
}

func TestMalformedRequests(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	conn, s := raw(t, h)
	for line, code := range map[string]string{
		`not json`:                                    protocol.CodeBadRequest,
		`{"op":"status"}`:                             protocol.CodeBadRequest,
		`{"id":1,"op":"status","extra":true}`:         protocol.CodeBadRequest,
		`{"id":2,"op":"settings.set","args":{"x":1}}`: protocol.CodeBadRequest,
		`{"id":3,"op":"settings.set","args":[]}`:      protocol.CodeBadRequest,
		`{"id":4,"op":"status","args":{"verbose":1}}`: protocol.CodeBadRequest,
	} {
		fmt.Fprintln(conn, line)
		if !s.Scan() {
			t.Fatalf("%s: no reply", line)
		}
		var env protocol.Envelope
		json.Unmarshal(s.Bytes(), &env)
		if env.Error == nil || env.Error.Code != code {
			t.Errorf("%s: reply %s", line, s.Text())
		}
	}

	// A line past the limit ends the session.
	conn.Write([]byte(strings.Repeat("x", protocol.MaxLine+10) + "\n"))
	if s.Scan() {
		t.Fatalf("oversized line got %q", s.Text())
	}
}

func TestAClientThatStopsSendingStillGetsItsReplies(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	conn, s := raw(t, h)
	fmt.Fprintln(conn, `{"id":1,"op":"start"}`)
	fmt.Fprintln(conn, `{"id":2,"op":"rules.list"}`)
	conn.(*net.UnixConn).CloseWrite()
	got := map[uint64]bool{}
	for s.Scan() {
		var env protocol.Envelope
		json.Unmarshal(s.Bytes(), &env)
		if env.Error != nil {
			t.Fatalf("reply %s", s.Text())
		}
		got[env.ID] = true
	}
	if !got[1] || !got[2] {
		t.Fatalf("replies %v", got)
	}
}

func TestACancelledCallIsNeverSent(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Call(ctx, protocol.OpStart, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled start = %v", err)
	}
	call[protocol.Status](t, c, protocol.OpStatus, nil) // the daemon has seen everything sent before
	if _, running := h.core.Config(); running {
		t.Fatal("a cancelled start ran")
	}
}

func TestClientsAreBounded(t *testing.T) {
	h := serve(t, os.Getuid(), func(o *daemon.Options) { o.MaxClients = 1 })
	h.dial(t)
	_, err := client.Dial(context.Background(), h.socket)
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.CodeBusy {
		t.Fatalf("second client = %v", err)
	}
}

func TestShutdownStopsTheCore(t *testing.T) {
	h := serve(t, os.Getuid(), nil)
	c := h.dial(t)
	call[protocol.Status](t, c, protocol.OpStart, nil)
	h.cancel()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
		h.done <- nil // for the cleanup
	case <-time.After(5 * time.Second):
		t.Fatal("no shutdown")
	}
	if _, running := h.core.Config(); running {
		t.Fatal("core left running")
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client not disconnected")
	}
}
