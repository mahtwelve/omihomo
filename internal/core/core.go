// Package core is the boundary between the daemon and the proxy engine it
// hosts. The daemon owns settings, state and clients; a Core only runs what
// it is given. Nothing outside an implementation of this package knows which
// engine that is.
package core

import (
	"context"
	"errors"
	"net"

	"github.com/readeem/omihomo/internal/protocol"
)

// Config is everything a Core needs to run. The daemon builds it from its
// typed settings; a Core never reads settings of its own.
type Config struct {
	Mode         string
	TUN          bool
	TUNRedirect  bool
	Tailscale    bool
	TailnetProxy bool
}

// ErrNotRunning is returned by calls that need a running core.
var ErrNotRunning = errors.New("the proxy is not running")

// ErrNotFound is returned when a named group, proxy or connection does not
// exist in the running config.
var ErrNotFound = errors.New("not found")

// Core runs one proxy engine in-process.
//
// Apply starts the engine on cfg, or reloads a running engine onto it. A
// failed Apply leaves the previous config running, or the engine stopped if
// it was. Stop is idempotent. All other calls fail with ErrNotRunning while
// stopped. Implementations must be safe for concurrent use.
type Core interface {
	Apply(ctx context.Context, cfg Config) error
	Stop(ctx context.Context) error

	Groups(ctx context.Context) ([]protocol.Group, error)
	Select(ctx context.Context, group, proxy string) error
	// TestDelay tests proxies of group, every member when proxies is empty,
	// and maps each to its delay in milliseconds, 0 for a failure.
	TestDelay(ctx context.Context, group string, proxies []string) (map[string]int, error)

	Rules(ctx context.Context) ([]protocol.Rule, error)

	Connections(ctx context.Context) (protocol.Connections, error)
	// CloseConnections closes the given ids; ids that are already gone are
	// not an error. A nil slice closes every connection.
	CloseConnections(ctx context.Context, ids []string) error
	// Traffic is the throughput over the last second.
	Traffic(ctx context.Context) (protocol.Traffic, error)

	// DialContext opens a connection routed by the running config, as if a
	// local application had made it through the proxy.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}
