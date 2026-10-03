package session

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	ghostty "go.mitchellh.com/libghostty"
	"golang.org/x/sys/unix"
)

type terminal struct {
	master, slave *os.File
	mu            sync.Mutex
	em            *ghostty.Terminal
	formatter     *ghostty.Formatter
	done, stop    chan struct{}
	err           error
}

func newTerminal(t *testing.T) *terminal {
	t.Helper()
	m, s, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(s, &pty.Winsize{Cols: 100, Rows: 20}); err != nil {
		t.Fatal(err)
	}
	h := &terminal{master: m, slave: s, done: make(chan struct{}), stop: make(chan struct{})}
	var replies []byte
	h.em, err = ghostty.NewTerminal(ghostty.WithSize(100, 20), ghostty.WithWritePty(func(_ *ghostty.Terminal, data []byte) { replies = append(replies, data...) }))
	if err != nil {
		t.Fatal(err)
	}
	h.formatter, err = ghostty.NewFormatter(h.em, ghostty.WithFormatterTrim(true))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(h.done)
		buf := make([]byte, 65536)
		for {
			select {
			case <-h.stop:
				return
			default:
			}
			fds := []unix.PollFd{{Fd: int32(m.Fd()), Events: unix.POLLIN}}
			if _, err := unix.Poll(fds, 20); err != nil && !errors.Is(err, unix.EINTR) {
				h.fail(err)
				return
			}
			if fds[0].Revents&unix.POLLIN == 0 {
				continue
			}
			n, err := unix.Read(int(m.Fd()), buf)
			if err != nil {
				h.fail(err)
				return
			}
			h.mu.Lock()
			_, err = h.em.Write(buf[:n])
			pending := replies
			replies = nil
			h.mu.Unlock()
			if err != nil {
				h.fail(err)
				return
			}
			if len(pending) > 0 {
				if _, err := m.Write(pending); err != nil {
					h.fail(err)
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(h.stop)
		<-h.done
		h.mu.Lock()
		h.formatter.Close()
		h.em.Close()
		err := h.err
		h.mu.Unlock()
		if err != nil {
			t.Error(err)
		}
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return h
}

func (h *terminal) fail(err error) { h.mu.Lock(); h.err = err; h.mu.Unlock() }
func (h *terminal) text() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.formatter.FormatString()
	if err != nil {
		return err.Error()
	}
	return s
}
func (h *terminal) send(t *testing.T, s string) {
	t.Helper()
	if _, err := h.master.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}
func (h *terminal) await(t *testing.T, s string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(h.text(), s) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("terminal missing %q: %q", s, h.text())
}
