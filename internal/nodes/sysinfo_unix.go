//go:build linux || darwin || freebsd

package nodes

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func collectPlatform(info *SystemInfo) {
	var uts unix.Utsname
	if unix.Uname(&uts) == nil {
		info.OSVersion = unix.ByteSliceToString(uts.Sysname[:]) + " " + unix.ByteSliceToString(uts.Release[:])
	}
	if runtime.GOOS == "linux" {
		readMeminfo(info)
		if data, err := os.ReadFile("/proc/uptime"); err == nil {
			if f, err := strconv.ParseFloat(strings.Fields(string(data))[0], 64); err == nil {
				info.UptimeSeconds = int64(f)
			}
		}
	} else {
		info.Unavailable = append(info.Unavailable, "memory", "uptime")
	}
	if d, ok := diskUsage("/"); ok {
		info.Disks = append(info.Disks, d)
	}
}

func readMeminfo(info *SystemInfo) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		info.Unavailable = append(info.Unavailable, "memory")
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			info.MemoryTotal = kb * 1024
		case "MemAvailable:":
			info.MemoryAvailable = kb * 1024
		}
	}
}

func diskUsage(path string) (Disk, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Disk{}, false
	}
	bsize := uint64(st.Bsize)                                                  //nolint:unconvert // Bsize differs in type across platforms.
	return usage(path, uint64(st.Blocks)*bsize, uint64(st.Bavail)*bsize), true //nolint:unconvert // Field types differ across platforms.
}
