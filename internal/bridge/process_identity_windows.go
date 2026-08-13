//go:build windows

package bridge

import (
	"fmt"
	"strconv"
	"syscall"
	"unsafe"
)

var (
	kernel32ProcessIdentity     = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcessIdentity     = kernel32ProcessIdentity.NewProc("OpenProcess")
	procGetProcessTimesIdentity = kernel32ProcessIdentity.NewProc("GetProcessTimes")
	procCloseHandleIdentity     = kernel32ProcessIdentity.NewProc("CloseHandle")
)

const processQueryLimitedInformation = 0x1000

func currentProcessStartMarker() (string, error) {
	return processStartMarker(syscall.Getpid())
}

func processIdentityMatches(pid int, marker string) (bool, error) {
	actual, err := processStartMarker(pid)
	if err != nil {
		return false, nil
	}
	return actual == marker, nil
}

func processStartMarker(pid int) (string, error) {
	handle, _, openErr := procOpenProcessIdentity.Call(processQueryLimitedInformation, 0, uintptr(uint32(pid)))
	if handle == 0 {
		return "", fmt.Errorf("open process %d: %v", pid, openErr)
	}
	defer procCloseHandleIdentity.Call(handle)
	var creation, exit, kernel, user syscall.Filetime
	ok, _, timesErr := procGetProcessTimesIdentity.Call(handle, uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return "", fmt.Errorf("get process times %d: %v", pid, timesErr)
	}
	value := uint64(uint32(creation.HighDateTime))<<32 | uint64(uint32(creation.LowDateTime))
	return strconv.FormatUint(value, 16), nil
}
