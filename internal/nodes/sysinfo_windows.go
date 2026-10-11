package nodes

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32Sysinfo          = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32Sysinfo.NewProc("GlobalMemoryStatusEx")
	procGetTickCount64       = kernel32Sysinfo.NewProc("GetTickCount64")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func collectPlatform(info *SystemInfo) {
	v := windows.RtlGetVersion()
	info.OSVersion = fmt.Sprintf("Windows %d.%d build %d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	var mem memoryStatusEx
	mem.Length = uint32(unsafe.Sizeof(mem))
	if ok, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem))); ok != 0 {
		info.MemoryTotal, info.MemoryAvailable = mem.TotalPhys, mem.AvailPhys
	} else {
		info.Unavailable = append(info.Unavailable, "memory")
	}
	if ms, _, _ := procGetTickCount64.Call(); ms != 0 {
		info.UptimeSeconds = int64(ms / 1000)
	}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		info.Unavailable = append(info.Unavailable, "disks")
		return
	}
	for i := range 26 {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		ptr, _ := windows.UTF16PtrFromString(root)
		// Only fixed disks: querying removable or network drives can block.
		if windows.GetDriveType(ptr) != windows.DRIVE_FIXED {
			continue
		}
		if d, ok := diskUsage(root); ok {
			info.Disks = append(info.Disks, d)
		}
	}
}

func diskUsage(path string) (Disk, bool) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Disk{}, false
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &free, &total, &totalFree); err != nil {
		return Disk{}, false
	}
	return usage(path, total, free), true
}
