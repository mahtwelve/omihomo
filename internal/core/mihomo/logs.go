package mihomo

import (
	"log/slog"
	"sync"
	"time"

	mlog "github.com/metacubex/mihomo/log"
)

// logTap is the one subscriber to mihomo's log. mihomo blocks every log
// call, and with it whatever code logged, until each subscriber has taken
// the event, so the tap only ever remembers the latest error. mihomo prints
// every event at its configured level to stdout itself, which the journal
// collects.
type logTap struct {
	mu      sync.Mutex
	changed *sync.Cond
	seq     uint64
	last    string
}

func startLogTap(log *slog.Logger) *logTap {
	t := &logTap{}
	t.changed = sync.NewCond(&t.mu)
	sub := mlog.Subscribe()
	go func() {
		for event := range sub {
			if event.LogLevel < mlog.ERROR {
				continue
			}
			t.mu.Lock()
			t.seq++
			t.last = event.Payload
			t.mu.Unlock()
			t.changed.Broadcast()
		}
		log.Error("mihomo's log closed")
	}()
	return t
}

// mark is a point in the log that errorSince can wait past.
func (t *logTap) mark() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seq
}

// errorSince returns the latest error mihomo logged after mark, waiting up
// to wait for one to arrive. It is "" when none did.
func (t *logTap) errorSince(mark uint64, wait time.Duration) string {
	timer := time.AfterFunc(wait, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.changed.Broadcast()
	})
	defer timer.Stop()
	deadline := time.Now().Add(wait)
	t.mu.Lock()
	defer t.mu.Unlock()
	for t.seq == mark && time.Now().Before(deadline) {
		t.changed.Wait()
	}
	if t.seq == mark {
		return ""
	}
	return t.last
}
