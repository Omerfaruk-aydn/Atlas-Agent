// Package android drives Android phones and emulators through the official
// Android Debug Bridge. Every device command names its serial explicitly
// (`adb -s <serial>`), there is no implicit default device, and coordinates
// are only accepted when they come from a fresh observation of the same
// screen geometry and foreground app.
package android

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// LocateError explains why no usable adb was found.
type LocateError struct {
	Checked []string
}

func (e *LocateError) Error() string {
	return "adb (Android SDK platform-tools) was not found or did not run. Checked: " + strings.Join(e.Checked, "; ") +
		". Install Android SDK Platform-Tools from https://developer.android.com/tools/releases/platform-tools (or Android Studio), then set tools.devices.adb_path or ANDROID_SDK_ROOT. Atlas does not download the SDK."
}

func adbName() string {
	if runtime.GOOS == "windows" {
		return "adb.exe"
	}
	return "adb"
}

// Locate finds a working adb: explicit config path, then ANDROID_SDK_ROOT
// and ANDROID_HOME platform-tools, then PATH. A candidate counts only when
// it exists and `adb version` succeeds.
func Locate(ctx context.Context, configured string) (string, error) {
	var candidates []string
	if configured != "" {
		candidates = append(candidates, configured)
	}
	for _, env := range []string{"ANDROID_SDK_ROOT", "ANDROID_HOME"} {
		if root := os.Getenv(env); root != "" {
			candidates = append(candidates, filepath.Join(root, "platform-tools", adbName()))
		}
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates, filepath.Join(local, "Android", "Sdk", "platform-tools", adbName()))
		}
	}
	if p, err := exec.LookPath(adbName()); err == nil {
		candidates = append(candidates, p)
	}
	var checked []string
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		info, err := os.Stat(c)
		if err != nil || info.IsDir() {
			checked = append(checked, c+" (missing)")
			continue
		}
		vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := exec.CommandContext(vctx, c, "version").CombinedOutput()
		cancel()
		if err != nil || !bytes.Contains(out, []byte("Android Debug Bridge")) {
			checked = append(checked, c+" (does not run)")
			continue
		}
		return c, nil
	}
	if len(checked) == 0 {
		checked = append(checked, "no tools.devices.adb_path, ANDROID_SDK_ROOT, ANDROID_HOME or adb on PATH")
	}
	return "", &LocateError{Checked: checked}
}

// State is a normalized device connection state.
type State string

const (
	StateReady        State = "ready"
	StateUnauthorized State = "unauthorized"
	StateOffline      State = "offline"
	StateDisconnected State = "disconnected"
	StateUnknown      State = "unknown"
)

// Device is one line of `adb devices -l`.
type Device struct {
	Serial      string `json:"serial"`
	State       State  `json:"state"`
	RawState    string `json:"raw_state"`
	Model       string `json:"model,omitempty"`
	Product     string `json:"product,omitempty"`
	DeviceName  string `json:"device,omitempty"`
	TransportID string `json:"transport_id,omitempty"`
	Emulator    bool   `json:"emulator"`
	Hint        string `json:"hint,omitempty"`
}

// ParseDevices parses `adb devices -l` output.
func ParseDevices(out string) []Device {
	var devices []Device
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "adb ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := Device{Serial: fields[0], RawState: fields[1]}
		switch fields[1] {
		case "device":
			d.State = StateReady
		case "unauthorized":
			d.State = StateUnauthorized
			d.Hint = "Unlock the device and accept the \"Allow USB debugging?\" prompt, then list devices again."
		case "offline":
			d.State = StateOffline
			d.Hint = "The device is connected but not responding; reconnect the cable or restart the emulator. Run \"adb reconnect\" only with the user's consent."
		case "no", "disconnected":
			d.State = StateDisconnected
		default:
			d.State = StateUnknown
		}
		for _, f := range fields[2:] {
			k, v, ok := strings.Cut(f, ":")
			if !ok {
				continue
			}
			switch k {
			case "model":
				d.Model = v
			case "product":
				d.Product = v
			case "device":
				d.DeviceName = v
			case "transport_id":
				d.TransportID = v
			}
		}
		d.Emulator = strings.HasPrefix(d.Serial, "emulator-")
		devices = append(devices, d)
	}
	return devices
}

// ADB runs adb commands.
type ADB struct {
	Path    string
	Timeout time.Duration
}

// ErrNoDevice is returned when a serial is not attached.
var ErrNoDevice = errors.New("device not attached")

// DeviceError classifies device problems for the tool contract.
type DeviceError struct {
	Code    string
	Serial  string
	Message string
}

func (e *DeviceError) Error() string { return e.Code + ": " + e.Message }

func (a *ADB) run(ctx context.Context, args ...string) ([]byte, error) {
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, a.Path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limited{b: &stdout, n: 32 << 20}, &limited{b: &stderr, n: 64 << 10}
	err := cmd.Run()
	if cctx.Err() != nil && ctx.Err() == nil {
		return stdout.Bytes(), fmt.Errorf("adb %s timed out after %s", strings.Join(redactArgs(args), " "), timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return stdout.Bytes(), classifyADBError(args, msg, err)
	}
	return stdout.Bytes(), nil
}

func redactArgs(args []string) []string {
	out := slices.Clone(args)
	for i := range out {
		if i > 0 && (out[i-1] == "text" || out[i-1] == "--es") {
			out[i] = "<redacted>"
		}
	}
	return out
}

func classifyADBError(args []string, msg string, err error) error {
	serial := ""
	if len(args) > 1 && args[0] == "-s" {
		serial = args[1]
	}
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "unauthorized"):
		return &DeviceError{Code: "device_unauthorized", Serial: serial, Message: "the device has not authorized this computer for USB debugging; accept the prompt on the device"}
	case strings.Contains(low, "offline"):
		return &DeviceError{Code: "device_offline", Serial: serial, Message: "the device is offline"}
	case strings.Contains(low, "not found") && strings.Contains(low, "device"):
		return &DeviceError{Code: "device_disconnected", Serial: serial, Message: "the device " + serial + " is not attached"}
	case strings.Contains(low, "more than one device"):
		return &DeviceError{Code: "ambiguous_device", Serial: serial, Message: "more than one device is attached; a serial is required"}
	}
	if msg == "" {
		msg = err.Error()
	}
	return &DeviceError{Code: "adb_failed", Serial: serial, Message: msg}
}

type limited struct {
	b *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.b.Len(); room > 0 {
		if len(p) > room {
			l.b.Write(p[:room])
		} else {
			l.b.Write(p)
		}
	}
	return len(p), nil
}

// Devices lists attached devices and emulators.
func (a *ADB) Devices(ctx context.Context) ([]Device, error) {
	out, err := a.run(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return ParseDevices(string(out)), nil
}

// Shell runs a device shell command with explicit serial targeting.
func (a *ADB) Shell(ctx context.Context, serial string, args ...string) (string, error) {
	out, err := a.run(ctx, append([]string{"-s", serial, "shell"}, args...)...)
	return string(out), err
}

// ExecOut runs a binary-safe device command (screencap, uiautomator dump).
func (a *ADB) ExecOut(ctx context.Context, serial string, args ...string) ([]byte, error) {
	return a.run(ctx, append([]string{"-s", serial, "exec-out"}, args...)...)
}

// Raw runs an adb subcommand for a serial (install, push, pull).
func (a *ADB) Raw(ctx context.Context, serial string, args ...string) (string, error) {
	out, err := a.run(ctx, append([]string{"-s", serial}, args...)...)
	return string(out), err
}

// RequireReady checks that serial is attached and authorized. It is the
// guard every device action calls first, so two devices can never be
// confused and an unauthorized device fails with a clear code.
func (a *ADB) RequireReady(ctx context.Context, serial string) (Device, error) {
	if serial == "" {
		return Device{}, &DeviceError{Code: "serial_required", Message: "every Android action needs an explicit serial from devices"}
	}
	devices, err := a.Devices(ctx)
	if err != nil {
		return Device{}, err
	}
	for _, d := range devices {
		if d.Serial == serial {
			switch d.State {
			case StateReady:
				return d, nil
			case StateUnauthorized:
				return d, &DeviceError{Code: "device_unauthorized", Serial: serial, Message: d.Hint}
			case StateOffline:
				return d, &DeviceError{Code: "device_offline", Serial: serial, Message: d.Hint}
			default:
				return d, &DeviceError{Code: "device_" + string(d.State), Serial: serial, Message: "device state is " + d.RawState}
			}
		}
	}
	return Device{}, &DeviceError{Code: "device_disconnected", Serial: serial, Message: "device " + serial + " is not attached; list devices again"}
}
