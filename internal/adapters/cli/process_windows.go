//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

func configureCommand(command *exec.Cmd) {
	// Keep npm shims, provider CLIs and taskkill from creating a visible
	// console window when the Controller is hosted by a desktop app.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}
