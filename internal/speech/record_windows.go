//go:build windows

package speech

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	winmm         = syscall.NewLazyDLL("winmm.dll")
	mciSendString = winmm.NewProc("mciSendStringW")
	mciGetError   = winmm.NewProc("mciGetErrorStringW")
	recordID      atomic.Uint64
)

func hideWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func mci(command string) error {
	p, err := syscall.UTF16PtrFromString(command)
	if err != nil {
		return err
	}
	code, _, _ := mciSendString.Call(uintptr(unsafe.Pointer(p)), 0, 0, 0)
	if code == 0 {
		return nil
	}
	var text [256]uint16
	mciGetError.Call(code, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	return fmt.Errorf("microphone: %s (MCI %d)", syscall.UTF16ToString(text[:]), code)
}

func record(ctx context.Context, path string, o DictationOptions, stop <-chan struct{}, ready chan<- error) error {
	if err := ctx.Err(); err != nil {
		ready <- err
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	alias := "atlasvoice" + strconv.FormatUint(recordID.Add(1), 10)
	if err := mci("open new type waveaudio alias " + alias); err != nil {
		ready <- err
		return err
	}
	defer func() { _ = mci("close " + alias) }()
	if err := mci("set " + alias + " format tag pcm channels 1 samplespersec 16000 bitspersample 16 alignment 2 bytespersec 32000"); err != nil {
		ready <- err
		return err
	}
	if err := ctx.Err(); err != nil {
		ready <- err
		return err
	}
	if err := mci("record " + alias); err != nil {
		ready <- err
		return err
	}
	ready <- nil
	timer := time.NewTimer(o.Duration())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-stop:
	case <-timer.C:
	}
	if err := mci("stop " + alias); err != nil {
		return err
	}
	return mci("save " + alias + " \"" + path + "\"")
}
