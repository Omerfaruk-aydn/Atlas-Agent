//go:build windows

package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/mgr"
)

var sandboxProcesses = []string{"windowssandbox.exe", "windowssandboxclient.exe", "windowssandboxremotesession.exe", "windowssandboxserver.exe"}

type windowsPlatform struct{}

// DefaultPlatform returns the native platform.
func DefaultPlatform() Platform { return windowsPlatform{} }

func (windowsPlatform) Probe(ctx context.Context) Facts {
	f := Facts{GOOS: "windows"}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		f.Edition, _, _ = k.GetStringValue("EditionID")
		if b, _, err := k.GetStringValue("CurrentBuildNumber"); err == nil {
			f.Build, _ = strconv.Atoi(b)
		}
		k.Close()
	}
	f.SandboxExe = filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsSandbox.exe")
	if _, err := os.Stat(f.SandboxExe); err == nil {
		f.SandboxExeFound = true
	}
	if m, err := mgr.Connect(); err == nil {
		if s, err := m.OpenService("vmms"); err == nil {
			if st, err := s.Query(); err == nil && st.State == 4 { // SERVICE_RUNNING
				f.HyperVServiceRunning = true
			}
			s.Close()
		}
		m.Disconnect()
	}
	// CIM is the documented way to read these two flags without admin.
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"[pscustomobject]@{h=(Get-CimInstance Win32_ComputerSystem).HypervisorPresent;v=@(Get-CimInstance Win32_Processor)[0].VirtualizationFirmwareEnabled}|ConvertTo-Json -Compress")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.Output(); err == nil {
		var v struct {
			H bool `json:"h"`
			V bool `json:"v"`
		}
		if json.Unmarshal(out, &v) == nil {
			f.HypervisorPresent, f.VirtualizationFW = v.H, v.V
		}
	} else {
		f.ProbeError = "virtualization probe failed: " + err.Error()
	}
	for _, p := range listProcesses() {
		if slices.Contains(sandboxProcesses, strings.ToLower(p.name)) {
			f.RunningSandboxPIDs = append(f.RunningSandboxPIDs, p.pid)
		}
	}
	return f
}

type procEntry struct {
	pid  int
	name string
}

func listProcesses() []procEntry {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []procEntry
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out = append(out, procEntry{pid: int(e.ProcessID), name: windows.UTF16ToString(e.ExeFile[:])})
	}
	return out
}

// Launch starts WindowsSandbox.exe and records every sandbox process that
// appeared after the launch, so Stop can end exactly those.
func (p windowsPlatform) Launch(ctx context.Context, wsb string) ([]procown.Identity, error) {
	before := map[int]bool{}
	for _, e := range listProcesses() {
		before[e.pid] = true
	}
	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsSandbox.exe")
	// The launcher hands off to the sandbox service and exits; the sandbox
	// lifetime is tracked through the owned identities below, not ctx.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exe, wsb)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(45 * time.Second)
	var owned []procown.Identity
	seen := map[int]bool{}
	for time.Now().Before(deadline) {
		for _, e := range listProcesses() {
			if before[e.pid] || seen[e.pid] || !slices.Contains(sandboxProcesses, strings.ToLower(e.name)) {
				continue
			}
			if created, err := procown.CreationTime(e.pid); err == nil && created > 0 {
				seen[e.pid] = true
				owned = append(owned, procown.Identity{PID: e.pid, CreatedAt: created})
			}
		}
		if slices.ContainsFunc(owned, func(id procown.Identity) bool { return isClient(id.PID) }) {
			return owned, nil
		}
		select {
		case <-ctx.Done():
			return owned, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if len(owned) == 0 {
		return nil, errors.New("no sandbox process appeared within 45 seconds of starting Windows Sandbox")
	}
	return owned, nil
}

func isClient(pid int) bool {
	for _, e := range listProcesses() {
		if e.pid == pid {
			return strings.EqualFold(e.name, "WindowsSandboxClient.exe") || strings.EqualFold(e.name, "WindowsSandboxRemoteSession.exe")
		}
	}
	return false
}

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
)

func (windowsPlatform) Focus(owned []procown.Identity) error {
	pids := map[uint32]bool{}
	for _, o := range owned {
		if procown.Matches(o) {
			pids[uint32(o.PID)] = true
		}
	}
	var target uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible != 0 && pids[pid] {
			target = hwnd
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	if target == 0 {
		return errors.New("no visible sandbox window found")
	}
	procShowWindow.Call(target, 9) // SW_RESTORE
	if ok, _, _ := procSetForegroundWindow.Call(target); ok == 0 {
		return errors.New("the sandbox window could not be brought to the front; click it in the taskbar")
	}
	return nil
}

func (windowsPlatform) HostSnapshot() (HostSnapshot, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	var pt struct{ X, Y int32 }
	if ok, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); ok == 0 {
		return HostSnapshot{}, err
	}
	buf := make([]uint16, 256)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return HostSnapshot{ForegroundWindow: hwnd, ForegroundTitle: windows.UTF16ToString(buf), CursorX: int(pt.X), CursorY: int(pt.Y)}, nil
}
