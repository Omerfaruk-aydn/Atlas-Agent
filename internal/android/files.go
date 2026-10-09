package android

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// InstallResult records what was installed.
type InstallResult struct {
	APK     string `json:"apk"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Package string `json:"package,omitempty"`
	Output  string `json:"output"`
}

// Install installs a local APK. The caller resolves and authorizes the
// path; this checks that it is a regular .apk file, records its hash and
// verifies adb reported Success.
func (c *Controller) Install(ctx context.Context, serial, apk string, replace bool) (InstallResult, error) {
	if !strings.EqualFold(path.Ext(strings.ReplaceAll(apk, "\\", "/")), ".apk") {
		return InstallResult{}, errors.New("install accepts only a local .apk file")
	}
	info, err := os.Stat(apk)
	if err != nil || !info.Mode().IsRegular() {
		return InstallResult{}, fmt.Errorf("APK %s is not a regular file", apk)
	}
	sum, err := transfer.HashFile(apk)
	if err != nil {
		return InstallResult{}, err
	}
	if _, err := c.ADB.RequireReady(ctx, serial); err != nil {
		return InstallResult{}, err
	}
	args := []string{"install"}
	if replace {
		args = append(args, "-r")
	}
	out, err := c.ADB.Raw(ctx, serial, append(args, apk)...)
	res := InstallResult{APK: apk, SHA256: sum, Size: info.Size(), Output: strings.TrimSpace(out)}
	if err != nil {
		return res, err
	}
	if !strings.Contains(out, "Success") {
		return res, fmt.Errorf("adb install did not report Success: %s", res.Output)
	}
	return res, nil
}

var devicePathBad = []string{"..", "\x00", "`", "$", ";", "&", "|", "'", "\"", "\n"}

// ValidDevicePath accepts absolute device paths under /sdcard or
// /data/local/tmp without shell metacharacters.
func ValidDevicePath(p string) error {
	if !strings.HasPrefix(p, "/sdcard/") && !strings.HasPrefix(p, "/data/local/tmp/") && !strings.HasPrefix(p, "/storage/emulated/0/") {
		return fmt.Errorf("device path %q must be under /sdcard/, /storage/emulated/0/ or /data/local/tmp/", p)
	}
	for _, bad := range devicePathBad {
		if strings.Contains(p, bad) {
			return fmt.Errorf("device path %q contains %q", p, bad)
		}
	}
	return nil
}

// deviceSHA256 hashes a file on the device with toybox sha256sum.
func (c *Controller) deviceSHA256(ctx context.Context, serial, p string) (string, error) {
	out, err := c.ADB.Shell(ctx, serial, "sha256sum", p)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 || !transfer.ValidSHA256(fields[0]) {
		return "", fmt.Errorf("device sha256sum unavailable: %s", strings.TrimSpace(out))
	}
	return fields[0], nil
}

// Push copies a local file to the device and verifies the device-side hash.
func (c *Controller) Push(ctx context.Context, serial, local, remote string) (string, error) {
	if err := ValidDevicePath(remote); err != nil {
		return "", err
	}
	sum, err := transfer.HashFile(local)
	if err != nil {
		return "", err
	}
	if _, err := c.ADB.RequireReady(ctx, serial); err != nil {
		return "", err
	}
	if _, err := c.ADB.Raw(ctx, serial, "push", local, remote); err != nil {
		return "", err
	}
	got, err := c.deviceSHA256(ctx, serial, remote)
	if err != nil {
		return sum, fmt.Errorf("pushed, but the device copy could not be verified: %w", err)
	}
	if got != sum {
		return sum, fmt.Errorf("hash_mismatch: device has %s, local file is %s", got, sum)
	}
	return sum, nil
}

// Pull copies a device file into root/rel and verifies it against the
// device-side hash before it is kept.
func (c *Controller) Pull(ctx context.Context, serial, remote, root, rel string, overwrite bool) (string, error) {
	if err := ValidDevicePath(remote); err != nil {
		return "", err
	}
	if _, err := c.ADB.RequireReady(ctx, serial); err != nil {
		return "", err
	}
	want, err := c.deviceSHA256(ctx, serial, remote)
	if err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "atlas-adb-pull-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	tmp := tmpDir + string(os.PathSeparator) + "pulled"
	if _, err := c.ADB.Raw(ctx, serial, "pull", remote, tmp); err != nil {
		return "", err
	}
	f, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	return transfer.Receive(root, rel, f, info.Size(), want, 0, overwrite)
}
