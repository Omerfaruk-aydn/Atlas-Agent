//go:build windows

package clipboard

import (
	"encoding/binary"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// The reader is raw syscall code against a format no dependency covers, so it
// is verified by a real round trip: put a CF_HDROP on the clipboard exactly as
// Explorer does, then read it back.
//
// These tests replace the machine's clipboard contents while they run.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procCopyMemory       = kernel32.NewProc("RtlMoveMemory")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
)

const gmemMoveable = 0x0002

// dropFiles mirrors the Win32 DROPFILES header that prefixes a CF_HDROP
// payload. The paths follow it, each NUL-terminated, with a second NUL
// closing the list.
type dropFiles struct {
	pFiles uint32
	x, y   int32
	fNC    int32
	fWide  int32
}

// writeClipboardFiles publishes paths as CF_HDROP, the way a file manager does.
func writeClipboardFiles(t *testing.T, paths []string) {
	t.Helper()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var list []uint16
	for _, p := range paths {
		enc, err := syscall.UTF16FromString(p)
		require.NoError(t, err)
		list = append(list, enc...)
	}
	list = append(list, 0) // the extra terminator closing the list

	header := dropFiles{pFiles: uint32(unsafe.Sizeof(dropFiles{})), fWide: 1}
	size := int(header.pFiles) + len(list)*2

	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	require.NotZero(t, hMem, "GlobalAlloc failed")

	p, _, _ := procGlobalLock.Call(hMem)
	require.NotZero(t, p, "GlobalLock failed")
	// Encode in Go-owned memory, then copy through the native ABI. The
	// foreign allocation address remains an integer, never a Go pointer.
	payload := make([]byte, size)
	binary.LittleEndian.PutUint32(payload[:4], header.pFiles)
	binary.LittleEndian.PutUint32(payload[16:20], uint32(header.fWide))
	for i, value := range list {
		binary.LittleEndian.PutUint16(payload[int(header.pFiles)+2*i:], value)
	}
	procCopyMemory.Call(p, uintptr(unsafe.Pointer(unsafe.SliceData(payload))), uintptr(size)) //nolint:errcheck
	runtime.KeepAlive(payload)
	procGlobalUnlock.Call(hMem) //nolint:errcheck

	var opened bool
	for range openAttempts {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			opened = true
			break
		}
		time.Sleep(openBackoff)
	}
	if !opened {
		procGlobalFree.Call(hMem) //nolint:errcheck
		t.Skip("another process is holding the clipboard open")
	}
	defer procCloseClipboard.Call() //nolint:errcheck

	procEmptyClipboard.Call() //nolint:errcheck
	// On success the clipboard owns the block, so it must not be freed here.
	r, _, err := procSetClipboardData.Call(cfHDrop, hMem)
	if r == 0 {
		procGlobalFree.Call(hMem) //nolint:errcheck
		require.Fail(t, "SetClipboardData failed", "%v", err)
	}
}

func TestReadFilesRoundTrip(t *testing.T) {
	want := []string{
		filepath.Join(t.TempDir(), "shot.png"),
		filepath.Join(t.TempDir(), "second image.jpg"),
	}
	writeClipboardFiles(t, want)

	got, err := readFiles()
	require.NoError(t, err)
	require.Equal(t, want, got, "paths must survive the round trip, spaces included")
}

// A path outside ASCII is the case a byte-wise or ANSI reader gets wrong, and
// this machine's own home directory is one.
func TestReadFilesHandlesUnicodePaths(t *testing.T) {
	want := []string{`C:\Users\Ömer&Ceylin\görüntü şəkil.png`}
	writeClipboardFiles(t, want)

	got, err := readFiles()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Text on the clipboard is not a file list, and must not be reported as one.
func TestReadFilesRejectsNonFileClipboard(t *testing.T) {
	writeText("just some text")

	_, err := readFiles()
	require.ErrorIs(t, err, ErrEmpty)
}
