package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/readeem/omihomo/internal/protocol"
)

// session is one connected owner client.
type session struct {
	d    *Daemon
	conn net.Conn

	out       chan []byte
	done      chan struct{}
	closeOnce sync.Once
	inFlight  chan struct{}
	calls     sync.WaitGroup

	// streams is guarded by d.mu.
	streams map[string]bool
}

func newSession(d *Daemon, conn net.Conn) *session {
	return &session{
		d:        d,
		conn:     conn,
		out:      make(chan []byte, sendQueue),
		done:     make(chan struct{}),
		inFlight: make(chan struct{}, maxInFlight),
		streams:  map[string]bool{},
	}
}

func (s *session) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.conn.Close()
	})
}

func (s *session) wants(stream string) bool { return s.streams[stream] }

// send queues one message. A client that lets its queue fill is
// disconnected rather than allowed to hold the daemon's memory.
func (s *session) send(v any) {
	b, err := protocol.Line(v)
	if err != nil {
		s.d.log.Error("encode message", "err", err)
		return
	}
	s.sendLine(b)
}

func (s *session) sendLine(b []byte) {
	select {
	case <-s.done:
	case s.out <- b:
	default:
		s.d.log.Warn("disconnected a client that stopped reading")
		s.close()
	}
}

// serve runs the session until the client leaves, misbehaves, or ctx ends.
// A client that stops sending still gets the replies to what it asked.
func (s *session) serve(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.close()
	go func() {
		select {
		case <-ctx.Done():
			s.close()
		case <-s.done:
		}
	}()
	drain := make(chan struct{})
	written := make(chan struct{})
	go func() {
		defer close(written)
		s.write(drain)
	}()
	defer func() {
		s.calls.Wait()
		close(drain)
		<-written
	}()

	scanner := protocol.NewScanner(s.conn)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req protocol.Request
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || dec.More() {
			s.reply(0, nil, &protocol.Error{Code: protocol.CodeBadRequest, Message: "not a request"})
			continue
		}
		if req.ID == 0 || req.Op == "" {
			s.reply(req.ID, nil, &protocol.Error{Code: protocol.CodeBadRequest, Message: "a request needs an id and an op"})
			continue
		}
		select {
		case s.inFlight <- struct{}{}:
		default:
			s.reply(req.ID, nil, &protocol.Error{Code: protocol.CodeBusy, Message: "too many calls in flight"})
			continue
		}
		s.calls.Go(func() {
			defer func() { <-s.inFlight }()
			callCtx, callCancel := context.WithTimeout(ctx, callTimeout)
			defer callCancel()
			result, perr := s.d.handle(callCtx, s, req)
			s.reply(req.ID, result, perr)
		})
	}
}

func (s *session) reply(id uint64, result any, err *protocol.Error) {
	r := protocol.Reply{Type: protocol.TypeReply, ID: id, Error: err}
	if err == nil {
		if result == nil {
			result = protocol.Empty{}
		}
		r.Result = result
	}
	s.send(r)
}

// write sends queued messages until the session closes, or until drain
// closes and the queue is empty.
func (s *session) write(drain <-chan struct{}) {
	put := func(b []byte) bool {
		s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := s.conn.Write(b); err != nil {
			s.close()
			return false
		}
		return true
	}
	for {
		select {
		case <-s.done:
			return
		case b := <-s.out:
			if !put(b) {
				return
			}
		case <-drain:
			for {
				select {
				case b := <-s.out:
					if !put(b) {
						return
					}
				default:
					return
				}
			}
		}
	}
}
