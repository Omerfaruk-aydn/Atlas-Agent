package android

// These are controlled protocol tests: the test binary stands in for adb
// and models a device's state. They prove argument construction, serial
// targeting and stale-coordinate rejection; they are not device runs.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeState struct {
	Devices  string `json:"devices"`
	Size     string `json:"size"`
	Rotation int    `json:"rotation"`
	Focus    string `json:"focus"`
	IME      string `json:"ime"`
	UI       string `json:"ui"`
	Log      string `json:"log"`
}

const sampleUI = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?><hierarchy rotation="0">
<node index="0" text="" resource-id="" class="android.widget.FrameLayout" package="com.example.form" content-desc="" clickable="false" enabled="true" focused="false" password="false" bounds="[0,0][1080,2400]">
<node index="0" text="Ada" resource-id="com.example.form:id/name" class="android.widget.EditText" package="com.example.form" content-desc="" clickable="true" enabled="true" focused="true" password="false" bounds="[40,300][1040,420]"/>
<node index="1" text="secret" resource-id="com.example.form:id/pin" class="android.widget.EditText" package="com.example.form" content-desc="" clickable="true" enabled="true" focused="false" password="true" bounds="[40,450][1040,570]"/>
<node index="2" text="Submit" resource-id="com.example.form:id/submit" class="android.widget.Button" package="com.example.form" content-desc="" clickable="true" enabled="true" focused="false" password="false" bounds="[40,600][1040,720]"/>
</node></hierarchy>UI hierchary dumped to: /dev/tty`

func TestMain(m *testing.M) {
	if statePath := os.Getenv("FAKE_ADB_STATE"); statePath != "" {
		os.Exit(fakeADB(statePath, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeADB(statePath string, args []string) int {
	data, _ := os.ReadFile(statePath)
	var st fakeState
	_ = json.Unmarshal(data, &st)
	if st.Log != "" {
		f, _ := os.OpenFile(st.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}
	if len(args) == 0 {
		return 1
	}
	switch args[0] {
	case "version":
		fmt.Println("Android Debug Bridge version 1.0.41")
		return 0
	case "devices":
		fmt.Print(st.Devices)
		return 0
	case "-s":
		serial := args[1]
		ready := false
		for _, d := range ParseDevices(st.Devices) {
			if d.Serial == serial && d.State == StateReady {
				ready = true
			}
		}
		if !ready {
			fmt.Fprintf(os.Stderr, "error: device '%s' not found\n", serial)
			return 1
		}
		rest := strings.Join(args[2:], " ")
		switch {
		case rest == "shell wm size":
			fmt.Println(st.Size)
		case rest == "shell wm density":
			fmt.Println("Physical density: 420")
		case rest == "shell dumpsys input":
			fmt.Printf("  SurfaceOrientation: %d\n", st.Rotation)
		case rest == "shell dumpsys window":
			fmt.Printf("  mCurrentFocus=Window{abc u0 %s}\n", st.Focus)
		case rest == "exec-out uiautomator dump /dev/tty":
			fmt.Print(st.UI)
		case rest == "exec-out screencap -p":
			var b bytes.Buffer
			_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)))
			os.Stdout.Write(b.Bytes())
		case rest == "shell settings get secure default_input_method":
			fmt.Println(st.IME)
		case strings.HasPrefix(rest, "shell input "), strings.HasPrefix(rest, "shell am broadcast"), strings.HasPrefix(rest, "shell monkey"):
			fmt.Println("Events injected: 1")
		default:
			fmt.Fprintln(os.Stderr, "fake adb: unsupported "+rest)
			return 1
		}
		return 0
	}
	return 1
}

type fakeDevice struct {
	t     *testing.T
	state fakeState
	path  string
	adb   *ADB
}

func newFake(t *testing.T) *fakeDevice {
	t.Helper()
	dir := t.TempDir()
	f := &fakeDevice{t: t, path: filepath.Join(dir, "state.json"), state: fakeState{
		Devices: "List of devices attached\nemulator-5554\tdevice product:sdk_gphone64 model:sdk_gphone64_x86_64 device:emu64x transport_id:1\nR58M123ABC\tunauthorized usb:1-1 transport_id:2\n0123456789\toffline transport_id:3\n",
		Size:    "Physical size: 1080x2400", Focus: "com.example.form/com.example.form.MainActivity", UI: sampleUI, Log: filepath.Join(dir, "log.txt"),
	}}
	f.save()
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("FAKE_ADB_STATE", f.path)
	f.adb = &ADB{Path: exe}
	return f
}

func (f *fakeDevice) save() {
	data, _ := json.Marshal(f.state)
	require.NoError(f.t, os.WriteFile(f.path, data, 0o644))
}

func (f *fakeDevice) commands() []string {
	data, _ := os.ReadFile(f.state.Log)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func inputCommands(cmds []string) []string {
	var out []string
	for _, c := range cmds {
		if strings.Contains(c, " shell input ") || strings.Contains(c, " am broadcast") {
			out = append(out, c)
		}
	}
	return out
}

func TestParseDevicesStates(t *testing.T) {
	t.Parallel()
	devices := ParseDevices("List of devices attached\nemulator-5554\tdevice product:p model:Pixel_7 device:d transport_id:4\nABC\tunauthorized\nDEF\toffline\n\n")
	require.Len(t, devices, 3)
	require.Equal(t, StateReady, devices[0].State)
	require.True(t, devices[0].Emulator)
	require.Equal(t, "Pixel_7", devices[0].Model)
	require.Equal(t, StateUnauthorized, devices[1].State)
	require.NotEmpty(t, devices[1].Hint)
	require.Equal(t, StateOffline, devices[2].State)
}

func TestParsers(t *testing.T) {
	t.Parallel()
	w, h, err := ParseSize("Physical size: 1080x2400\nOverride size: 720x1600")
	require.NoError(t, err)
	require.Equal(t, [2]int{720, 1600}, [2]int{w, h}, "input uses the override size")
	require.Equal(t, 420, ParseDensity("Physical density: 420"))
	r, ok := ParseRotation("  SurfaceOrientation: 1")
	require.True(t, ok)
	require.Equal(t, 1, r)
	require.Equal(t, Focus{Package: "com.android.settings", Activity: ".Settings"}, ParseFocus("mCurrentFocus=Window{1a2b u0 com.android.settings/.Settings}"))
	els, err := ParseUIDump([]byte(sampleUI))
	require.NoError(t, err)
	require.Len(t, els, 4)
	require.Empty(t, els[2].Text, "password text must never be exposed")
	x, y := els[3].Center()
	require.Equal(t, [2]int{540, 660}, [2]int{x, y})
}

func TestEveryDeviceCommandNamesTheSerial(t *testing.T) {
	f := newFake(t)
	c := NewController(f.adb)
	obs, err := c.Observe(t.Context(), "emulator-5554", true, true)
	require.NoError(t, err)
	_, _, err = c.Tap(t.Context(), "emulator-5554", obs.ID, Target{ResourceID: "com.example.form:id/submit"})
	require.NoError(t, err)
	for _, cmd := range f.commands() {
		if strings.HasPrefix(cmd, "version") || strings.HasPrefix(cmd, "devices") {
			continue
		}
		require.True(t, strings.HasPrefix(cmd, "-s emulator-5554 "), "command without explicit serial: %q", cmd)
	}
	require.Contains(t, inputCommands(f.commands()), "-s emulator-5554 shell input tap 540 660")
}

func TestUnauthorizedOfflineAndMissingDevicesFailClearly(t *testing.T) {
	f := newFake(t)
	c := NewController(f.adb)
	var de *DeviceError
	_, err := c.Observe(t.Context(), "R58M123ABC", true, false)
	require.ErrorAs(t, err, &de)
	require.Equal(t, "device_unauthorized", de.Code)
	_, err = c.Observe(t.Context(), "0123456789", true, false)
	require.ErrorAs(t, err, &de)
	require.Equal(t, "device_offline", de.Code)
	_, err = c.Observe(t.Context(), "gone", true, false)
	require.ErrorAs(t, err, &de)
	require.Equal(t, "device_disconnected", de.Code)
	_, err = c.Observe(t.Context(), "", true, false)
	require.ErrorAs(t, err, &de)
	require.Equal(t, "serial_required", de.Code)
	require.Empty(t, inputCommands(f.commands()))
}

func TestRotationOrAppSwitchInvalidatesCoordinates(t *testing.T) {
	f := newFake(t)
	c := NewController(f.adb)
	obs, err := c.Observe(t.Context(), "emulator-5554", true, false)
	require.NoError(t, err)

	f.state.Rotation = 1
	f.save()
	_, _, err = c.Tap(t.Context(), "emulator-5554", obs.ID, Target{X: 100, Y: 100})
	var stale *StaleError
	require.ErrorAs(t, err, &stale)
	require.Contains(t, stale.Reason, "rotated")

	f.state.Rotation = 0
	f.state.Focus = "com.other.app/.Main"
	f.save()
	_, _, err = c.Tap(t.Context(), "emulator-5554", obs.ID, Target{X: 100, Y: 100})
	require.ErrorAs(t, err, &stale)
	require.Contains(t, stale.Reason, "foreground app changed")
	require.Empty(t, inputCommands(f.commands()), "no input may be sent with a stale observation")

	// Another device's observation can never aim this device.
	_, _, err = c.Tap(t.Context(), "emulator-5555", obs.ID, Target{X: 1, Y: 1})
	require.Error(t, err)
}

func TestObservationIsConsumedByTapAndBoundsAreChecked(t *testing.T) {
	f := newFake(t)
	c := NewController(f.adb)
	obs, err := c.Observe(t.Context(), "emulator-5554", true, false)
	require.NoError(t, err)
	_, _, err = c.Tap(t.Context(), "emulator-5554", obs.ID, Target{X: 5000, Y: 10})
	require.ErrorContains(t, err, "outside")
	_, _, err = c.Tap(t.Context(), "emulator-5554", obs.ID, Target{Element: 99})
	require.Error(t, err)
	_, _, err = c.Tap(t.Context(), "emulator-5554", obs.ID, Target{Element: 4})
	require.NoError(t, err)
	_, _, err = c.Tap(t.Context(), "emulator-5554", obs.ID, Target{Element: 4})
	var stale *StaleError
	require.ErrorAs(t, err, &stale, "a consumed observation cannot be replayed")
}

func TestUnicodeTextIsRefusedWithoutADBKeyboard(t *testing.T) {
	f := newFake(t)
	c := NewController(f.adb)
	method, err := c.TypeText(t.Context(), "emulator-5554", "Ada Lovelace")
	require.NoError(t, err)
	require.Equal(t, TextInputCommand, method)
	require.Contains(t, inputCommands(f.commands()), "-s emulator-5554 shell input text Ada%sLovelace")

	_, err = c.TypeText(t.Context(), "emulator-5554", "Şükrü Öztürk")
	var ue *UnicodeError
	require.ErrorAs(t, err, &ue)
	require.Len(t, inputCommands(f.commands()), 1, "nothing may be typed when the method cannot deliver the text")

	_, err = c.TypeText(t.Context(), "emulator-5554", "a;rm -rf /")
	require.ErrorAs(t, err, &ue, "shell metacharacters are not passed to input text")

	f.state.IME = adbKeyboardIME
	f.save()
	method, err = c.TypeText(t.Context(), "emulator-5554", "Şükrü Öztürk")
	require.NoError(t, err)
	require.Equal(t, TextADBKeyboard, method)
	last := inputCommands(f.commands())
	require.True(t, strings.Contains(last[len(last)-1], "ADB_INPUT_B64"))
}

func TestVerifyTextUsesAFreshDump(t *testing.T) {
	f := newFake(t)
	c := NewController(f.adb)
	el, _, err := c.FindText(t.Context(), "emulator-5554", "Ada", Target{ResourceID: "com.example.form:id/name"})
	require.NoError(t, err)
	require.NotNil(t, el)
	el, _, err = c.FindText(t.Context(), "emulator-5554", "secret", Target{})
	require.NoError(t, err)
	require.Nil(t, el, "password text is not matchable")
}

func TestDevicePathValidation(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidDevicePath("/sdcard/Download/a.txt"))
	for _, p := range []string{"/system/bin/sh", "/sdcard/../data", "/sdcard/a;b", "relative"} {
		require.Error(t, ValidDevicePath(p), p)
	}
}

func TestLocateReportsCheckedPaths(t *testing.T) {
	t.Setenv("ANDROID_SDK_ROOT", filepath.Join(t.TempDir(), "missing-sdk"))
	t.Setenv("ANDROID_HOME", "")
	t.Setenv("PATH", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", t.TempDir())
	}
	_, err := Locate(t.Context(), "")
	var le *LocateError
	require.True(t, errors.As(err, &le))
	require.True(t, slices.ContainsFunc(le.Checked, func(s string) bool { return strings.Contains(s, "missing-sdk") }))
	require.Contains(t, err.Error(), "does not download")
}
