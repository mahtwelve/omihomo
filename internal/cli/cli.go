// Package cli is the omihomo command: the daemon itself, and the owner's
// subcommands, each one call over the daemon's socket.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/readeem/omihomo/internal/client"
	"github.com/readeem/omihomo/internal/protocol"
)

// Version is set at build time with -ldflags "-X ...cli.Version=v2.0.0".
var Version = "dev"

// Exit codes.
const (
	ExitOK          = 0
	ExitFailure     = 1
	ExitUsage       = 2
	ExitUnreachable = 10 // no daemon answers on the socket
	ExitState       = 11 // the call contradicts the current state
	ExitNotFound    = 12
	ExitForbidden   = 13 // this account does not own the daemon
	ExitVersion     = 14 // the daemon speaks another protocol version
	ExitUnsupported = 15
)

const usage = `usage: omihomo <command> [--pretty]

  status
  start | stop
  set mode <rule|global|direct>
  set tun|tun-redirect|tailscale|tailnet-proxy <on|off>
  set group <name>             primary group; "" for the default
  sub list | add | update <name> | activate <name> | remove <name>
                               add reads the URL from stdin
  rule list | add <type> <value> <target> | remove <index>
  group list | select <group> <proxy> | test <group> [proxy...]
  conn list | close <id>... | close --all
  egress
  watch [stream...]            print pushed events as JSON lines
  version
  daemon [flags]               run the daemon (see omihomo daemon -h)
`

// Env is what a command reads and writes besides its arguments.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Run executes one command and returns its exit code.
func Run(ctx context.Context, args []string, env Env) int {
	pretty := false
	var rest []string
	for _, a := range args {
		if a == "--pretty" {
			pretty = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] == "-h" || rest[0] == "--help" || rest[0] == "help" {
		fmt.Fprint(env.Stderr, usage)
		if len(rest) == 0 {
			return ExitUsage
		}
		return ExitOK
	}
	switch rest[0] {
	case "daemon":
		return runDaemon(ctx, rest[1:], env)
	case "version":
		fmt.Fprintf(env.Stdout, "omihomo %s (protocol %d)\n", Version, protocol.Version)
		return ExitOK
	case "watch":
		return watch(ctx, rest[1:], env)
	}

	op, callArgs, err := parse(rest, env.Stdin)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omihomo: %v\n", err)
		return ExitUsage
	}
	c, code := dial(ctx, env)
	if c == nil {
		return code
	}
	defer c.Close()
	var result json.RawMessage
	if err := c.Call(ctx, op, callArgs, &result); err != nil {
		return report(env, err)
	}
	return printResult(env, result, pretty)
}

// parse maps a command line onto one protocol call.
func parse(args []string, stdin io.Reader) (string, any, error) {
	cmd, rest := args[0], args[1:]
	want := func(n int, shape string) error {
		if len(rest) != n {
			return fmt.Errorf("usage: omihomo %s %s", cmd, shape)
		}
		return nil
	}
	switch cmd {
	case "status", "start", "stop", "egress":
		if err := want(0, ""); err != nil {
			return "", nil, err
		}
		return map[string]string{"status": protocol.OpStatus, "start": protocol.OpStart,
			"stop": protocol.OpStop, "egress": protocol.OpEgress}[cmd], nil, nil

	case "set":
		if len(rest) != 2 {
			return "", nil, errors.New("usage: omihomo set <setting> <value>")
		}
		return parseSet(rest[0], rest[1])

	case "sub":
		if len(rest) == 0 {
			return "", nil, errors.New("usage: omihomo sub list|add|update|activate|remove")
		}
		verb, rest := rest[0], rest[1:]
		switch verb {
		case "list":
			if len(rest) == 0 {
				return protocol.OpSubscriptions, nil, nil
			}
		case "add":
			if len(rest) == 0 {
				url, err := readURL(stdin)
				return protocol.OpSubscriptionAdd, protocol.SubscriptionURLArgs{URL: url}, err
			}
			return "", nil, errors.New("sub add reads the URL from stdin, so it never shows in a process list")
		case "update", "activate", "remove":
			if len(rest) == 1 {
				op := map[string]string{"update": protocol.OpSubscriptionUpdate,
					"activate": protocol.OpSubscriptionUse, "remove": protocol.OpSubscriptionRemove}[verb]
				return op, protocol.NameArgs{Name: rest[0]}, nil
			}
		}
		return "", nil, errors.New("usage: omihomo sub list | add | update|activate|remove <name>")

	case "rule":
		switch {
		case len(rest) == 1 && rest[0] == "list":
			return protocol.OpRules, nil, nil
		case len(rest) == 4 && rest[0] == "add":
			return protocol.OpRuleAdd, protocol.RuleArgs{Type: rest[1], Value: rest[2], Target: rest[3]}, nil
		case len(rest) == 2 && rest[0] == "remove":
			n, err := strconv.Atoi(rest[1])
			if err != nil || n < 1 {
				return "", nil, errors.New("a rule index is a positive number")
			}
			return protocol.OpRuleRemove, protocol.IndexArgs{Index: n}, nil
		}
		return "", nil, errors.New("usage: omihomo rule list | add <type> <value> <target> | remove <index>")

	case "group":
		switch {
		case len(rest) == 1 && rest[0] == "list":
			return protocol.OpGroups, nil, nil
		case len(rest) == 3 && rest[0] == "select":
			return protocol.OpSelect, protocol.SelectArgs{Group: rest[1], Proxy: rest[2]}, nil
		case len(rest) >= 2 && rest[0] == "test":
			return protocol.OpDelayTest, protocol.DelayArgs{Group: rest[1], Proxies: rest[2:]}, nil
		}
		return "", nil, errors.New("usage: omihomo group list | select <group> <proxy> | test <group> [proxy...]")

	case "conn":
		switch {
		case len(rest) == 1 && rest[0] == "list":
			return protocol.OpConnections, nil, nil
		case len(rest) == 2 && rest[0] == "close" && rest[1] == "--all":
			return protocol.OpCloseConnections, protocol.CloseArgs{All: true}, nil
		case len(rest) >= 2 && rest[0] == "close":
			return protocol.OpCloseConnections, protocol.CloseArgs{IDs: rest[1:]}, nil
		}
		return "", nil, errors.New("usage: omihomo conn list | close <id>... | close --all")
	}
	return "", nil, fmt.Errorf("unknown command %q; see omihomo --help", cmd)
}

func parseSet(name, value string) (string, any, error) {
	var args protocol.SettingsArgs
	switch name {
	case "mode":
		args.Mode = &value
	case "group":
		args.PrimaryGroup = &value
	case "tun", "tun-redirect", "tailscale", "tailnet-proxy":
		var on bool
		switch value {
		case "on":
			on = true
		case "off":
		default:
			return "", nil, fmt.Errorf("set %s takes on or off", name)
		}
		switch name {
		case "tun":
			args.TUN = &on
		case "tun-redirect":
			args.TUNRedirect = &on
		case "tailscale":
			args.Tailscale = &on
		case "tailnet-proxy":
			args.TailnetProxy = &on
		}
	default:
		return "", nil, fmt.Errorf("unknown setting %q", name)
	}
	return protocol.OpSetSettings, args, nil
}

func readURL(stdin io.Reader) (string, error) {
	if stdin == nil {
		return "", errors.New("sub add reads the URL from stdin")
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 8192))
	if err != nil {
		return "", err
	}
	url := strings.TrimSpace(string(raw))
	if url == "" {
		return "", errors.New("sub add reads the URL from stdin, and it was empty")
	}
	return url, nil
}

func dial(ctx context.Context, env Env) (*client.Client, int) {
	c, err := client.Dial(ctx, client.SocketPath())
	if err != nil {
		var perr *protocol.Error
		var verr *client.VersionError
		switch {
		case errors.As(err, &perr), errors.As(err, &verr):
			return nil, report(env, err)
		}
		fmt.Fprintf(env.Stderr, "omihomo: the daemon is not running or not installed (%v)\n", err)
		return nil, ExitUnreachable
	}
	return c, ExitOK
}

// report prints err and maps it to an exit code.
func report(env Env, err error) int {
	fmt.Fprintf(env.Stderr, "omihomo: %v\n", err)
	var verr *client.VersionError
	if errors.As(err, &verr) {
		return ExitVersion
	}
	var perr *protocol.Error
	if !errors.As(err, &perr) {
		return ExitFailure
	}
	switch perr.Code {
	case protocol.CodeBadRequest, protocol.CodeInvalid, protocol.CodeUnknownOp:
		return ExitUsage
	case protocol.CodeNotRunning, protocol.CodeConflict:
		return ExitState
	case protocol.CodeNotFound:
		return ExitNotFound
	case protocol.CodeForbidden:
		return ExitForbidden
	case protocol.CodeUnsupported:
		return ExitUnsupported
	}
	return ExitFailure
}

// printResult writes a result as one JSON line, or indented with pretty. An empty
// result prints nothing.
func printResult(env Env, result json.RawMessage, pretty bool) int {
	if len(result) == 0 || string(result) == "{}" {
		return ExitOK
	}
	var buf bytes.Buffer
	var err error
	if pretty {
		err = json.Indent(&buf, result, "", "  ")
	} else {
		err = json.Compact(&buf, result)
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "omihomo: malformed result: %v\n", err)
		return ExitFailure
	}
	buf.WriteByte('\n')
	env.Stdout.Write(buf.Bytes())
	return ExitOK
}

func watch(ctx context.Context, streams []string, env Env) int {
	if len(streams) == 0 {
		streams = []string{protocol.StreamStatus, protocol.StreamGroups, protocol.StreamRules,
			protocol.StreamTraffic, protocol.StreamConnections}
	}
	c, code := dial(ctx, env)
	if c == nil {
		return code
	}
	defer c.Close()
	if err := c.Call(ctx, protocol.OpSubscribe, protocol.StreamsArgs{Streams: streams}, nil); err != nil {
		return report(env, err)
	}
	enc := json.NewEncoder(env.Stdout)
	for {
		select {
		case <-ctx.Done():
			return ExitOK
		case ev, ok := <-c.Events():
			if !ok {
				fmt.Fprintln(env.Stderr, "omihomo: the daemon closed the connection")
				return ExitUnreachable
			}
			enc.Encode(struct {
				Stream string          `json:"stream"`
				Data   json.RawMessage `json:"data"`
			}{ev.Stream, ev.Data})
		}
	}
}

// Main is the binary's entry point.
func Main(ctx context.Context) int {
	return Run(ctx, os.Args[1:], Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
}
