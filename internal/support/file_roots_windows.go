package support

import (
	"golang.org/x/sys/windows"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

func computerRootCandidates() ([]nodes.Root, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	var roots []nodes.Root
	for i := range 26 {
		if mask&(1<<i) == 0 {
			continue
		}
		label := string(rune('A' + i))
		path := label + `:\`
		pointer, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return nil, err
		}
		switch windows.GetDriveType(pointer) {
		case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_RAMDISK:
			roots = append(roots, nodes.Root{Label: label, Path: path})
		}
	}
	return roots, nil
}
