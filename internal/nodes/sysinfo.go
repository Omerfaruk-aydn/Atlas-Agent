package nodes

import (
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"time"
)

// Disk is the capacity of one volume. Free space says nothing about the
// health of the device; a full disk and a failing disk are different
// diagnoses, and no health data is collected here.
type Disk struct {
	Path  string `json:"path"`
	Total uint64 `json:"total_bytes"`
	Free  uint64 `json:"free_bytes"`
	// UsedPercent is rounded to one decimal place.
	UsedPercent float64 `json:"used_percent"`
}

// NetInterface is one network adapter and its addresses.
type NetInterface struct {
	Name  string   `json:"name"`
	Up    bool     `json:"up"`
	Addrs []string `json:"addrs,omitempty"`
}

// SystemInfo is a read-only snapshot collected in-process, without running
// a shell, so it needs only the diagnostics permission.
type SystemInfo struct {
	NodeID          string         `json:"node_id"`
	SessionID       string         `json:"session_id,omitempty"`
	Generation      uint64         `json:"generation,omitempty"`
	Hostname        string         `json:"hostname"`
	OS              string         `json:"os"`
	OSVersion       string         `json:"os_version,omitempty"`
	Arch            string         `json:"arch"`
	CPUs            int            `json:"cpus"`
	UptimeSeconds   int64          `json:"uptime_seconds,omitempty"`
	MemoryTotal     uint64         `json:"memory_total_bytes,omitempty"`
	MemoryAvailable uint64         `json:"memory_available_bytes,omitempty"`
	User            string         `json:"user,omitempty"`
	Disks           []Disk         `json:"disks"`
	Interfaces      []NetInterface `json:"interfaces,omitempty"`
	CollectedAt     int64          `json:"collected_at"`
	// Unavailable names parts that could not be read on this platform.
	Unavailable []string `json:"unavailable,omitempty"`
}

// CollectSystemInfo gathers the snapshot. dirs adds the volumes holding
// the allowed folders to the platform's own volume list.
func CollectSystemInfo(dirs []string) SystemInfo {
	info := SystemInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), CollectedAt: time.Now().UnixMilli()}
	info.Hostname, _ = os.Hostname()
	if u, err := user.Current(); err == nil {
		info.User = u.Username
	}
	collectPlatform(&info)
	seen := map[string]bool{}
	for _, d := range info.Disks {
		seen[d.Path] = true
	}
	for _, dir := range dirs {
		vol := volumeOf(dir)
		if vol == "" || seen[vol] {
			continue
		}
		seen[vol] = true
		if d, ok := diskUsage(vol); ok {
			info.Disks = append(info.Disks, d)
		}
	}
	slices.SortFunc(info.Disks, func(a, b Disk) int {
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			row := NetInterface{Name: iface.Name, Up: iface.Flags&net.FlagUp != 0}
			if addrs, err := iface.Addrs(); err == nil {
				for _, a := range addrs {
					row.Addrs = append(row.Addrs, a.String())
				}
			}
			info.Interfaces = append(info.Interfaces, row)
		}
	}
	return info
}

func volumeOf(dir string) string {
	if v := filepath.VolumeName(dir); v != "" {
		return v + string(filepath.Separator)
	}
	if dir == "" {
		return ""
	}
	return dir
}

func usage(path string, total, free uint64) Disk {
	d := Disk{Path: path, Total: total, Free: free}
	if total > 0 {
		d.UsedPercent = float64(int64((float64(total-free)/float64(total))*1000+0.5)) / 10
	}
	return d
}
