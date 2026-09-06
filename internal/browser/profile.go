package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Browsing as the user means browsing with the user's cookies, and those
// live in the profile Chrome is already using. That profile cannot simply
// be opened: Chrome holds a lock on it while it runs, and since Chrome 136
// it refuses to open a debugging port on the default profile at all.
//
// So the profile is copied. A snapshot in a directory of our own is not
// the default profile as far as Chrome is concerned, which satisfies both
// constraints at once, and it carries the logins across because the
// credentials are encrypted per OS user rather than per directory.
//
// What a snapshot is not is a live view. Sign into something in the
// snapshot and the everyday browser knows nothing about it, and the other
// way round. Refreshing means taking the copy again, which is why the
// snapshot is keyed by directory: delete it and the next launch rebuilds
// it from whatever the profile looks like then.

// profileContents is what gets copied out of a Chrome profile. It is an
// allowlist rather than a skip-list because the great majority of a
// profile by size is cache -- 1.4 GB of a 1.5 GB profile is not unusual --
// and none of it is worth copying. Everything here is either a credential,
// a site's stored state, or something the browser would look wrong
// without.
var profileContents = []string{
	"Network",                // cookies, HSTS state
	"Login Data",             // saved passwords
	"Login Data For Account", //
	"Web Data",               // autofill
	"Preferences",            //
	"Secure Preferences",     //
	"Local Storage",          // where sites keep session tokens
	"Session Storage",        //
	"IndexedDB",              // and where the rest of them keep it
	"Affiliation Database",   //
	"Bookmarks",              //
}

// Extensions are deliberately absent. They are the bulk of a profile by
// a wide margin -- 616 MB of an 825 MB copy on the machine this was
// written against -- and none of it serves the point of the snapshot,
// which is arriving already signed in. Their stored settings are left
// behind with them, since settings without the extension are dead
// weight.

// snapshotRealProfile copies the user's active Chrome profile into a
// managed directory and returns that directory and the profile name
// within it. An existing snapshot is reused as-is; delete it to resync.
func snapshotRealProfile(dest, pin string) (string, error) {
	source, err := chromeUserDataDir()
	if err != nil {
		return "", err
	}

	profile := pin
	if profile == "" {
		if profile, err = lastUsedProfile(source); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(source, profile)); err != nil {
		return "", fmt.Errorf("browser profile %q not found in %s", profile, source)
	}

	// A snapshot already taken is left alone: rebuilding it on every
	// launch would copy hundreds of megabytes for nothing and would
	// throw away whatever the agent signed into last time.
	if _, err := os.Stat(filepath.Join(dest, "Local State")); err == nil {
		return profile, nil
	}

	if err := os.MkdirAll(filepath.Join(dest, profile), 0o755); err != nil {
		return "", fmt.Errorf("create profile snapshot: %w", err)
	}

	// Local State holds the key the cookie and password databases are
	// encrypted with, so a snapshot without it is a snapshot of nothing
	// readable.
	if err := copyPath(filepath.Join(source, "Local State"), filepath.Join(dest, "Local State")); err != nil {
		return "", fmt.Errorf("copy browser encryption state: %w", err)
	}

	for _, name := range profileContents {
		// Chrome keeps these files open, and a running browser will
		// have some of them locked. A missing piece degrades the
		// snapshot -- one site's stored session, say -- rather than
		// invalidating it, so a failed item is skipped and the rest
		// still gets copied. Closing Chrome first gives a clean copy.
		_ = copyPath(filepath.Join(source, profile, name), filepath.Join(dest, profile, name))
	}
	return profile, nil
}

// chromeUserDataDir locates the directory Chrome keeps its profiles in.
func chromeUserDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var candidates []string
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		candidates = []string{
			filepath.Join(local, "Google", "Chrome", "User Data"),
			filepath.Join(local, "Chromium", "User Data"),
		}
	case "darwin":
		support := filepath.Join(home, "Library", "Application Support")
		candidates = []string{
			filepath.Join(support, "Google", "Chrome"),
			filepath.Join(support, "Chromium"),
		}
	default:
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		candidates = []string{
			filepath.Join(config, "google-chrome"),
			filepath.Join(config, "chromium"),
		}
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
	}
	return "", errors.New("no Chrome profile directory found to copy; set tools.browser.user_data_dir instead")
}

// lastUsedProfile reports which profile the user actually browses with,
// which on a machine with several is not necessarily "Default". Getting
// this wrong means the agent quietly acts as the wrong person, so when it
// cannot be read the caller is told rather than guessed at.
func lastUsedProfile(userDataDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(userDataDir, "Local State"))
	if err != nil {
		return "", fmt.Errorf("read browser state: %w", err)
	}
	var state struct {
		Profile struct {
			LastUsed string `json:"last_used"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return "", fmt.Errorf("read browser state: %w", err)
	}
	if state.Profile.LastUsed == "" {
		return "Default", nil
	}
	return state.Profile.LastUsed, nil
}

// copyPath copies a file or a directory tree.
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	var firstErr error
	for _, entry := range entries {
		// Caches inside an otherwise wanted directory (an extension's
		// service worker, say) are still caches.
		if strings.Contains(strings.ToLower(entry.Name()), "cache") {
			continue
		}
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
