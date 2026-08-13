//go:build !windows

package bridge

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func currentProcessStartMarker() (string, error) {
	return processStartMarker(os.Getpid())
}

func processIdentityMatches(pid int, marker string) (bool, error) {
	actual, err := processStartMarker(pid)
	if err != nil {
		return false, nil
	}
	return actual == marker, nil
}

func processStartMarker(pid int) (string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if err := syscall.Kill(pid, 0); err != nil {
			return "", err
		}
		return "", fmt.Errorf("read process start marker: %w", err)
	}
	closing := strings.LastIndexByte(string(raw), ')')
	if closing < 0 {
		return "", fmt.Errorf("invalid process stat for %d", pid)
	}
	fields := strings.Fields(string(raw)[closing+1:])
	if len(fields) <= 19 {
		return "", fmt.Errorf("process stat for %d is incomplete", pid)
	}
	return fields[19], nil
}
