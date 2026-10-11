//go:build !windows && !linux && !darwin && !freebsd

package nodes

func collectPlatform(info *SystemInfo) {
	info.Unavailable = append(info.Unavailable, "memory", "uptime", "disks")
}

func diskUsage(string) (Disk, bool) { return Disk{}, false }
