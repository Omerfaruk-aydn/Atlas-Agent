//go:build windows

package activity

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const cursorRecoveryArgument = "--atlas-cursor-recovery"

// RunCursorRecovery restores configured cursors if the owner's pipe closes
// or its heartbeat stops. It runs before the normal CLI command dispatcher.
func RunCursorRecovery() bool {
	if len(os.Args) != 2 || os.Args[1] != cursorRecoveryArgument {
		return false
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	release, ok := acquireCursorOwnership()
	if !ok {
		_, _ = os.Stdout.Write([]byte{'B'})
		return true
	}
	defer release()
	watchCursorOwner(os.Stdin, os.Stdout, 8*time.Second, resetConfiguredCursors)
	return true
}

// The recovery process owns this session-wide mutex through restoration.
// A second Atlas process cannot preserve transparent copies from the first.
func acquireCursorOwnership() (func(), bool) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	name, _ := syscall.UTF16PtrFromString(`Local\AtlasAgentCursorOwnership.v1`)
	handle, _, _ := kernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, false
	}
	result, _, _ := kernel.NewProc("WaitForSingleObject").Call(handle, 0)
	if result != 0 && result != 0x80 {
		kernel.NewProc("CloseHandle").Call(handle)
		return nil, false
	}
	if result == 0x80 {
		resetConfiguredCursors()
	}
	return func() {
		kernel.NewProc("ReleaseMutex").Call(handle)
		kernel.NewProc("CloseHandle").Call(handle)
	}, true
}

func resetConfiguredCursors() {
	overlayUser.NewProc("SystemParametersInfoW").Call(0x57, 0, 0, 0)
}

type cursorGuard struct {
	input io.WriteCloser
	cmd   *exec.Cmd
}

func startCursorGuard() (*cursorGuard, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(context.Background(), executable, cursorRecoveryArgument)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		return nil, err
	}
	ready := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := io.ReadFull(output, b[:])
		if err == nil && b[0] != 'R' {
			err = fmt.Errorf("unexpected cursor watchdog response")
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err == nil {
			return &cursorGuard{input: input, cmd: cmd}, nil
		}
	case <-time.After(2 * time.Second):
	}
	_ = input.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return nil, fmt.Errorf("cursor recovery watchdog did not become ready")
}

func (g *cursorGuard) stop(restored bool) {
	if restored {
		_, _ = g.input.Write([]byte{'Q'})
	}
	_ = g.input.Close()
	go func() { _ = g.cmd.Wait() }()
}

// cursorOverride preserves the user's actual handles during normal cleanup.
// System-wide hiding only starts after an independent recovery process is ready.
type cursorOverride struct {
	saved     map[uint32]uintptr
	guard     *cursorGuard
	heartbeat time.Time
	disabled  bool
}

var systemCursorIDs = []uint32{32512, 32513, 32514, 32515, 32516, 32642, 32643, 32644, 32645, 32646, 32648, 32649, 32650, 32651}

func (c *cursorOverride) hide() error {
	if c.disabled || c.guard != nil {
		return nil
	}
	guard, err := startCursorGuard()
	if err != nil {
		c.disabled = true
		return err
	}
	c.guard = guard
	c.saved = make(map[uint32]uintptr)
	for _, id := range systemCursorIDs {
		original, _, _ := overlayUser.NewProc("LoadCursorW").Call(0, uintptr(id))
		copy, _, _ := overlayUser.NewProc("CopyImage").Call(original, 2, 0, 0, 0)
		if original == 0 || copy == 0 {
			c.restore()
			c.disabled = true
			return fmt.Errorf("cannot preserve system cursor %d", id)
		}
		c.saved[id] = copy
	}
	// Monochrome AND=1/XOR=0 produces a fully transparent cursor.
	andMask, xorMask := make([]byte, 128), make([]byte, 128)
	for i := range andMask {
		andMask[i] = 0xff
	}
	for _, id := range systemCursorIDs {
		hidden, _, _ := overlayUser.NewProc("CreateCursor").Call(0, 0, 0, 32, 32, uintptr(unsafe.Pointer(&andMask[0])), uintptr(unsafe.Pointer(&xorMask[0])))
		if hidden == 0 {
			c.restore()
			c.disabled = true
			return fmt.Errorf("cannot create transparent cursor")
		}
		ok, _, _ := overlayUser.NewProc("SetSystemCursor").Call(hidden, uintptr(id))
		if ok == 0 {
			overlayUser.NewProc("DestroyCursor").Call(hidden)
			c.restore()
			c.disabled = true
			return fmt.Errorf("cannot hide system cursor %d", id)
		}
	}
	c.heartbeat = time.Now()
	return nil
}

func (c *cursorOverride) restore() {
	if c.guard == nil {
		return
	}
	restored := true
	for id, cursor := range c.saved {
		ok, _, _ := overlayUser.NewProc("SetSystemCursor").Call(cursor, uintptr(id))
		if ok == 0 {
			overlayUser.NewProc("DestroyCursor").Call(cursor)
			restored = false
		}
	}
	if !restored {
		resetConfiguredCursors()
	}
	c.guard.stop(restored)
	c.saved, c.guard = nil, nil
}

func (c *cursorOverride) tick() {
	if c.guard == nil || time.Since(c.heartbeat) < time.Second {
		return
	}
	if _, err := c.guard.input.Write([]byte{'H'}); err != nil {
		c.restore()
		c.disabled = true
	}
	c.heartbeat = time.Now()
}
