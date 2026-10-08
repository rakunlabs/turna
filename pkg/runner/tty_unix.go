//go:build linux || darwin

package runner

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// A process-wide guard also protects commands started outside the registry.
var terminalOwner sync.Mutex

func startTTY(path string, args []string, attr *os.ProcAttr) (*os.Process, func(), error) {
	input, output := attr.Files[0], attr.Files[1]
	if !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return nil, nil, fmt.Errorf("tty requires terminal stdin and stdout")
	}
	if !terminalOwner.TryLock() {
		return nil, nil, fmt.Errorf("terminal is already attached to another service")
	}
	cleanup := func() { terminalOwner.Unlock() }
	fail := func(err error) (*os.Process, func(), error) {
		cleanup()
		return nil, nil, err
	}

	master, slave, err := pty.Open()
	if err != nil {
		return fail(fmt.Errorf("open pty: %w", err))
	}
	cleanup = func() { master.Close(); slave.Close(); terminalOwner.Unlock() }
	if err := pty.InheritSize(output, master); err != nil {
		return fail(fmt.Errorf("set terminal size: %w", err))
	}
	// Reopen a nonblocking descriptor so os.File uses the runtime poller;
	// Close can then interrupt a pending output read during shutdown.
	fd, err := unix.Dup(int(master.Fd()))
	if err != nil {
		return fail(err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return fail(err)
	}
	master.Close()
	master = os.NewFile(uintptr(fd), "service-pty")
	wakeR, wakeW, err := os.Pipe()
	if err != nil {
		return fail(err)
	}
	cleanup = func() {
		wakeR.Close()
		wakeW.Close()
		master.Close()
		slave.Close()
		terminalOwner.Unlock()
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return fail(fmt.Errorf("make terminal raw: %w", err))
	}
	previousCleanup := cleanup
	cleanup = func() { term.Restore(int(input.Fd()), state); previousCleanup() }

	attr.Files = []*os.File{slave, slave, slave}
	attr.Sys.Setpgid = false
	attr.Sys.Setsid = true
	attr.Sys.Setctty = true
	attr.Sys.Ctty = 0
	proc, err := os.StartProcess(path, args, attr)
	if err != nil {
		return fail(err)
	}
	slave.Close()

	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	stop := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for {
			select {
			case <-stop:
				return
			case <-resize:
				_ = resizeTTY(output, master)
			}
		}
	}()
	go func() {
		defer workers.Done()
		// Poll a wake pipe as well as stdin: never leave a goroutine consuming
		// terminal input after the service exits, and never close parent stdin.
		fds := []unix.PollFd{{Fd: int32(input.Fd()), Events: unix.POLLIN}, {Fd: int32(wakeR.Fd()), Events: unix.POLLIN}}
		buf := make([]byte, 4096)
		for {
			_, err := unix.Poll(fds, -1)
			if err == unix.EINTR {
				continue
			}
			if err != nil || fds[1].Revents != 0 {
				return
			}
			if fds[0].Revents != 0 {
				n, err := unix.Read(int(input.Fd()), buf)
				if n > 0 {
					if _, err := master.Write(buf[:n]); err != nil {
						return
					}
				}
				if err != nil || n == 0 {
					return
				}
			}
		}
	}()
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		_, _ = io.Copy(output, master)
	}()
	return proc, func() {
		signal.Stop(resize)
		close(stop)
		wakeW.Close()
		// Normally the slave closes on exit. Bound draining when a descendant
		// still holds it open, while preserving the command's final output.
		select {
		case <-outputDone:
		case <-time.After(200 * time.Millisecond):
		}
		master.Close()
		workers.Wait()
		<-outputDone
		cleanup()
	}, nil
}

func resizeTTY(from, to *os.File) error {
	size, err := pty.GetsizeFull(from)
	if err != nil {
		return err
	}
	raw, err := to.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	err = raw.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: size.Rows, Col: size.Cols})
	})
	if err != nil {
		return err
	}
	return ioctlErr
}
