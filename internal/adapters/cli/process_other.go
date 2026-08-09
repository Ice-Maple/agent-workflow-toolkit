//go:build !windows

package cli

import "os/exec"

func configureCommand(command *exec.Cmd) {}
