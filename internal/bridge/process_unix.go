//go:build !windows

package bridge

import (
	"bytes"
	"io"
	"os/exec"
	"syscall"
)

func configureDetachedProcess(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Stdin = bytes.NewReader(nil)
	if command.Stdout == nil {
		command.Stdout = io.Discard
	}
	if command.Stderr == nil {
		command.Stderr = io.Discard
	}
	return nil
}
