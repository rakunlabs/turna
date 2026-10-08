//go:build linux || darwin

package runner

import (
	"context"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func testTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Dup(int(master.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	master.Close()
	master = os.NewFile(uintptr(fd), "test-terminal")
	t.Cleanup(func() { master.Close(); slave.Close() })
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	return master, slave
}

func readTerminalUntil(t *testing.T, terminal *os.File, want string) string {
	t.Helper()
	if err := terminal.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	buf := make([]byte, 4096)
	for !strings.Contains(text.String(), want) {
		n, err := terminal.Read(buf)
		text.Write(buf[:n])
		if err != nil {
			t.Fatalf("waiting for %q: %v; output: %q", want, err, text.String())
		}
	}
	return text.String()
}

func TestTTYInputResizeAndRestore(t *testing.T) {
	master, slave := testTerminal(t)
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := &Command{Name: "tty", TTY: true, stdin: slave, stdout: slave, stderr: slave,
		Command: []string{"sh", "-c", "test -t 0 && test -t 1 && test -t 2 || exit 1; stty size; echo ready; read value; echo input:$value; while [ \"$(stty size)\" != \"40 120\" ]; do sleep 0.01; done; stty size; echo stderr-marker >&2"}}
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	initial := readTerminalUntil(t, master, "ready")
	if !strings.Contains(initial, "24 80") {
		t.Fatalf("initial size: %q", initial)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	if _, err := master.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	out := readTerminalUntil(t, master, "stderr-marker")
	if !strings.Contains(out, "input:hello") {
		t.Fatalf("input forwarding: %q", out)
	}
	if !strings.Contains(out, "40 120") {
		t.Fatalf("resized terminal: %q", out)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("terminal state was not restored")
	}
}

func TestTTYRejectsPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	c := &Command{Name: "pipe", TTY: true, stdin: r, stdout: w, Command: []string{"sh", "-c", "exit 0"}}
	if err := c.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "requires terminal") {
		t.Fatalf("expected terminal error, got %v", err)
	}
}

func TestTTYControlC(t *testing.T) {
	master, slave := testTerminal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := &Command{Name: "interrupt", TTY: true, stdin: slave, stdout: slave, stderr: slave,
		Command: []string{"sh", "-c", "trap 'echo interrupted; exit 0' INT; echo ready; while :; do sleep 1; done"}}
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	readTerminalUntil(t, master, "ready")
	if _, err := master.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	readTerminalUntil(t, master, "interrupted")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTTYCancelRestoresTerminal(t *testing.T) {
	master, slave := testTerminal(t)
	before, _ := term.GetState(int(slave.Fd()))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := &Command{Name: "cancel", TTY: true, stdin: slave, stdout: slave, stderr: slave,
		Command: []string{"sh", "-c", "echo ready; exec sleep 30"}}
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	readTerminalUntil(t, master, "ready")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tty shutdown hung")
	}
	after, _ := term.GetState(int(slave.Fd()))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("terminal state was not restored after cancellation")
	}
}

func TestTTYStartFailureRestoresTerminal(t *testing.T) {
	_, slave := testTerminal(t)
	before, _ := term.GetState(int(slave.Fd()))
	for range 2 {
		attr := &os.ProcAttr{Files: []*os.File{slave, slave, slave}, Sys: &syscall.SysProcAttr{Setpgid: true}}
		_, _, err := startTTY("/nonexistent-turna-test-command", []string{"missing"}, attr)
		if err == nil {
			t.Fatal("expected startup error")
		}
		after, _ := term.GetState(int(slave.Fd()))
		if !reflect.DeepEqual(before, after) {
			t.Fatal("terminal state was not restored after startup failure")
		}
	}
}

func TestRegistryOnlyOneTTY(t *testing.T) {
	s := NewStoreReg(&sync.WaitGroup{})
	if err := s.Add(&Command{Name: "first", TTY: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(&Command{Name: "second", TTY: true}); err == nil {
		t.Fatal("accepted two tty services")
	}
}
