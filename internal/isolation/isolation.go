// Package isolation runs desktop applications away from the user's own
// session. The first backend is Windows Sandbox: a disposable Hyper-V guest
// with its own session, input queue and screen. Atlas starts the sandbox
// with a generated .wsb file, a read-only copy of its own executable and one
// read-write exchange folder, and runs `atlas node serve` inside the guest as
// the control agent. All observation and input then travel over the node
// protocol to the guest's own desktop backend; the host's SendInput is never
// used for guest actions.
//
// Windows virtual desktops are deliberately not a backend: they share the
// user's session and input queue and cannot isolate mouse or keyboard.
package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Backend names.
const (
	BackendCurrentDesktop = "current_desktop"
	BackendWindowsSandbox = "windows_sandbox"
	BackendHyperVVM       = "hyperv_vm"
)

// Capability describes one environment backend on this machine.
type Capability struct {
	Backend   string `json:"backend"`
	Isolated  bool   `json:"isolated"`
	Available bool   `json:"available"`
	// Reason explains unavailability or the trade-off, in plain words.
	Reason        string   `json:"reason"`
	Prerequisites []string `json:"prerequisites,omitempty"`
	// AdminRequired is true when making the backend available needs an
	// administrator once (enabling a Windows feature). Atlas never does it.
	AdminRequired bool              `json:"admin_required,omitempty"`
	Facts         map[string]string `json:"facts,omitempty"`
}

// Facts are the probed host properties capability decisions are based on.
type Facts struct {
	GOOS                 string
	Edition              string
	Build                int
	SandboxExe           string
	SandboxExeFound      bool
	HypervisorPresent    bool
	VirtualizationFW     bool
	HyperVServiceRunning bool
	RunningSandboxPIDs   []int
	ProbeError           string
}

// sandboxEditions are the editions Microsoft documents for Windows Sandbox.
var sandboxEditions = map[string]bool{
	"Professional": true, "ProfessionalWorkstation": true, "ProfessionalEducation": true,
	"Enterprise": true, "EnterpriseS": true, "Education": true, "ServerRdsh": false,
}

// Evaluate turns probed facts into capabilities. It is pure so every
// decision is unit tested against documented constraints.
func Evaluate(f Facts) []Capability {
	out := []Capability{{
		Backend: BackendCurrentDesktop, Isolated: false, Available: f.GOOS == "windows",
		Reason: "The user's own desktop session. Not isolated: Atlas input moves the real mouse and keyboard and can steal focus. Use the computer tool for it.",
	}}
	sb := Capability{Backend: BackendWindowsSandbox, Isolated: true, Facts: map[string]string{
		"edition": f.Edition, "build": fmt.Sprint(f.Build), "hypervisor_present": fmt.Sprint(f.HypervisorPresent),
		"virtualization_firmware_enabled": fmt.Sprint(f.VirtualizationFW), "sandbox_exe": f.SandboxExe,
	}, Prerequisites: []string{
		"Windows 10/11 Pro, Enterprise or Education, build 18342 or newer",
		"Hardware virtualization enabled in the BIOS/UEFI firmware",
		"Optional feature \"Windows Sandbox\" (Containers-DisposableClientVM) enabled once by an administrator",
		"Only one Windows Sandbox can run at a time",
	}}
	switch {
	case f.GOOS != "windows":
		sb.Reason = "Windows Sandbox exists only on Windows."
	case f.Build > 0 && f.Build < 18342:
		sb.Reason = fmt.Sprintf("Windows build %d is older than 18342, which .wsb configuration needs.", f.Build)
	case f.Edition != "" && !sandboxEditions[f.Edition]:
		sb.Reason = fmt.Sprintf("Windows edition %q does not include Windows Sandbox (Home is not supported).", f.Edition)
	case !f.SandboxExeFound:
		sb.Reason = "The Windows Sandbox feature is not enabled (WindowsSandbox.exe is missing). An administrator can enable it in \"Turn Windows features on or off\"; Atlas will not change system features."
		sb.AdminRequired = true
	case !f.HypervisorPresent && !f.VirtualizationFW:
		sb.Reason = "Hardware virtualization is off: the hypervisor is not running and firmware virtualization is disabled. Enable Intel VT-x / AMD-V (SVM) in the BIOS/UEFI settings, then restart. Windows Sandbox cannot start until then."
	case !f.HypervisorPresent:
		sb.Reason = "The Windows hypervisor is not running yet. Restart Windows after enabling the Windows Sandbox feature."
	case len(f.RunningSandboxPIDs) > 0:
		sb.Available = false
		sb.Reason = "A Windows Sandbox is already running and only one instance is allowed. Close it, or reuse a sandbox Atlas started."
		sb.Facts["running"] = fmt.Sprint(f.RunningSandboxPIDs)
	default:
		sb.Available = true
		sb.Reason = "Disposable Hyper-V guest with its own session, input and screen. Closing it discards everything inside."
	}
	if f.ProbeError != "" {
		sb.Facts["probe_error"] = f.ProbeError
	}
	vm := Capability{
		Backend: BackendHyperVVM, Isolated: true, Available: false,
		Reason: "Atlas does not create virtual machines. To use a VM you prepared, run \"atlas node serve --allow exec,files,desktop --declare-isolated\" inside it and pair it as a node; the isolated_desktop tool can then target it with node=<name>.",
	}
	if f.HyperVServiceRunning {
		vm.Facts = map[string]string{"hyperv_management_service": "running"}
	}
	return append(out, sb, vm)
}

// Lease is Atlas's ownership record for one environment it started.
type Lease struct {
	ID        string `json:"id"`
	Backend   string `json:"backend"`
	Dir       string `json:"dir"`
	Exchange  string `json:"exchange"`
	ConfigWSB string `json:"config_wsb"`
	NodeName  string `json:"node_name"`
	// Owned are processes Atlas launched; only these are ever stopped.
	Owned       []procown.Identity `json:"owned"`
	Fingerprint string             `json:"fingerprint,omitempty"`
	Address     string             `json:"address,omitempty"`
	CreatedAt   int64              `json:"created_at"`
	Networking  bool               `json:"networking"`
	MemoryMB    int                `json:"memory_mb,omitempty"`
}

// ErrUnavailable means the isolated backend cannot run here. Callers must
// not fall back to the host desktop without the user's say-so.
var ErrUnavailable = errors.New("isolated environment unavailable")

// UnavailableError carries the capability explanation.
type UnavailableError struct{ Capability Capability }

func (e *UnavailableError) Error() string {
	return ErrUnavailable.Error() + ": " + e.Capability.Reason
}

func (e *UnavailableError) Unwrap() error { return ErrUnavailable }

// leaseStore persists leases and their lifecycle records.
type leaseStore struct {
	dir    string
	states *taskstate.Store
}

func newLeaseStore(dir string) *leaseStore {
	return &leaseStore{dir: dir, states: taskstate.NewStore(filepath.Join(dir, "states"))}
}

func (s *leaseStore) leaseDir(id string) string { return filepath.Join(s.dir, "leases", id) }

func (s *leaseStore) save(l Lease) error {
	return taskstate.WriteJSONAtomic(filepath.Join(s.leaseDir(l.ID), "lease.json"), l)
}

func (s *leaseStore) load(id string) (Lease, error) {
	if !taskstate.ValidID(id) {
		return Lease{}, fmt.Errorf("invalid lease id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(s.leaseDir(id), "lease.json"))
	if err != nil {
		return Lease{}, err
	}
	var l Lease
	return l, json.Unmarshal(data, &l)
}

func (s *leaseStore) list() ([]Lease, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "leases"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Lease
	for _, e := range entries {
		if l, err := s.load(e.Name()); err == nil {
			out = append(out, l)
		}
	}
	return out, nil
}

// waitFile polls for path until ctx ends. Readiness is a bounded poll of a
// concrete signal, never a fixed sleep.
func waitFile(ctx context.Context, path string, interval time.Duration) ([]byte, error) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}
