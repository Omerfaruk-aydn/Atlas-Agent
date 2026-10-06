//go:build windows

package browser

import (
	"encoding/binary"
	"runtime"
	"syscall"
	"unsafe"
)

var browserTCPTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// localBrowserListenerOwned rejects port forwarding and arbitrary loopback
// servers: the OS listener PID must equal the browser PID reported by CDP.
func localBrowserListenerOwned(port uint16, pid uint32) bool {
	for _, family := range []uintptr{2, 23} {
		var size uint32
		result, _, _ := browserTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, family, 3, 0)
		if result != 122 || size < 4 || size > 16*1024*1024 {
			continue
		}
		buffer := make([]byte, size)
		result, _, _ = browserTCPTable.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0, family, 3, 0)
		runtime.KeepAlive(buffer)
		if result == 0 && tcpTableHasBrowserListener(buffer, family == 23, port, pid) {
			return true
		}
	}
	return false
}

func tcpTableHasBrowserListener(data []byte, ipv6 bool, port uint16, pid uint32) bool {
	if len(data) < 4 || pid == 0 || port == 0 {
		return false
	}
	rowSize, portOffset, pidOffset := 24, 8, 20
	if ipv6 {
		rowSize, portOffset, pidOffset = 56, 20, 52
	}
	count := uint64(binary.LittleEndian.Uint32(data))
	if count > uint64((len(data)-4)/rowSize) {
		return false
	}
	for i := 0; i < int(count); i++ {
		row := data[4+i*rowSize : 4+(i+1)*rowSize]
		if binary.BigEndian.Uint16(row[portOffset:]) == port && binary.LittleEndian.Uint32(row[pidOffset:]) == pid {
			return true
		}
	}
	return false
}
