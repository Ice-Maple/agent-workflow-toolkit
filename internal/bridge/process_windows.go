//go:build windows

package bridge

import (
	"bytes"
	"io"
	"os/exec"
	"syscall"
)

func configureDetachedProcess(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.Stdin = bytes.NewReader(nil)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return nil
}
