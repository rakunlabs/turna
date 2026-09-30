//go:build unix

package sshserver

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixTerminal struct{ *os.File }

func (t *unixTerminal) Resize(cols, rows uint32) error {
	return pty.Setsize(t.File, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func startPTY(cmd *exec.Cmd, cols, rows uint32) (terminal, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &unixTerminal{File: f}, nil
}
