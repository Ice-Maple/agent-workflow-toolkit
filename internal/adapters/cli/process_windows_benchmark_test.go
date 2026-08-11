//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

const (
	processQueryLimitedInformation = 0x1000
	processVMRead                  = 0x0010
)

var (
	kernel32DLL          = syscall.NewLazyDLL("kernel32.dll")
	openProcessProc      = kernel32DLL.NewProc("OpenProcess")
	psapiDLL             = syscall.NewLazyDLL("psapi.dll")
	getProcessMemoryProc = psapiDLL.NewProc("GetProcessMemoryInfo")
)

type processMemoryCounters struct {
	Size                       uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

func BenchmarkRunnerWorkingSet(b *testing.B) {
	for _, memoryMiB := range []int{0, 32} {
		b.Run(fmt.Sprintf("Payload%dMiB", memoryMiB), func(b *testing.B) {
			workspace := b.TempDir()
			memoryBytes := memoryMiB * 1024 * 1024
			var workingSetTotal uint64
			var peakWorkingSetTotal uint64
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				command := helperBenchmarkCommand(workspace, "memory", "")
				command.Environment = append(command.Environment, "CLI_PROCESS_MEMORY_BYTES="+strconv.Itoa(memoryBytes))
				handle, err := NewRunner().Start(b.Context(), command)
				if err != nil {
					b.Fatal(err)
				}
				waitForBenchmarkLine(b, handle, fmt.Sprintf("CLI_MEMORY_READY %d", memoryBytes))
				workingSet, peakWorkingSet, err := queryProcessMemory(handle.PID())
				if err != nil {
					_ = handle.Cancel()
					_ = handle.Wait()
					b.Fatal(err)
				}
				workingSetTotal += uint64(workingSet)
				peakWorkingSetTotal += uint64(peakWorkingSet)
				cancel := handle.Cancel()
				result := handle.Wait()
				if cancel.Err != nil || !result.KillConfirmed {
					b.Fatalf("memory probe cleanup: cancel=%#v result=%#v", cancel, result)
				}
			}
			if b.N > 0 {
				b.ReportMetric(float64(workingSetTotal)/float64(b.N), "working_set_bytes")
				b.ReportMetric(float64(peakWorkingSetTotal)/float64(b.N), "peak_working_set_bytes")
			}
		})
	}
}

func BenchmarkInstalledCLIColdStart(b *testing.B) {
	probes := []struct {
		name    string
		command string
	}{
		{name: "Cursor", command: "cursor-agent.cmd"},
		{name: "Claude", command: "claude.cmd"},
		{name: "Codex", command: "codex.cmd"},
	}
	powerShell, err := exec.LookPath("powershell.exe")
	if err != nil {
		b.Skip("powershell.exe is unavailable")
	}
	for _, probe := range probes {
		probe := probe
		b.Run(probe.name, func(b *testing.B) {
			commandPath, err := exec.LookPath(probe.command)
			if err != nil {
				b.Skipf("%s is unavailable", probe.command)
			}
			scriptPath := strings.TrimSuffix(commandPath, filepath.Ext(commandPath)) + ".ps1"
			if _, err := os.Stat(scriptPath); err != nil {
				b.Skipf("PowerShell shim is unavailable: %v", err)
			}
			workspace := b.TempDir()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := NewRunner().Run(b.Context(), Command{
					Executable: powerShell,
					Args: []string{
						"-NoLogo", "-NoProfile", "-NonInteractive",
						"-ExecutionPolicy", "Bypass", "-File", scriptPath, "--version",
					},
					Dir: workspace,
				})
				if err != nil {
					b.Fatal(err)
				}
				if result.ExitCode != 0 || len(result.Stdout)+len(result.Stderr) == 0 {
					b.Fatalf("%s version probe = %#v", probe.name, result)
				}
			}
		})
	}
}

func queryProcessMemory(pid int) (workingSet uintptr, peakWorkingSet uintptr, err error) {
	process, _, callErr := openProcessProc.Call(
		processQueryLimitedInformation|processVMRead,
		0,
		uintptr(pid),
	)
	if process == 0 {
		return 0, 0, fmt.Errorf("open process %d: %w", pid, callErr)
	}
	defer syscall.CloseHandle(syscall.Handle(process))

	counters := processMemoryCounters{Size: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	ok, _, callErr := getProcessMemoryProc.Call(
		process,
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Size),
	)
	if ok == 0 {
		return 0, 0, fmt.Errorf("query process %d memory: %w", pid, callErr)
	}
	return counters.WorkingSetSize, counters.PeakWorkingSetSize, nil
}
