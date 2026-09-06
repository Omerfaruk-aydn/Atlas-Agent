package browser

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Attaching to a browser the user already runs is how the agent gets a
// signed-in session: cookies, saved logins and open tabs are the point,
// and a freshly launched throwaway profile has none of them.
//
// The catch is that Chrome will not open a debugging port on its default
// profile any more -- since Chrome 136 that is refused outright, to stop a
// page from talking any browser you happen to be running into handing over
// its cookies. So "attach to my everyday Chrome" is not a thing that can
// be built; what can be built is a browser of its own that stays signed in
// between runs, which this file launches on demand.
//
// Launching it here rather than asking the user to keep a terminal open is
// the difference between a setting they have to remember and one that just
// works: the agent asks for a page, the browser is there.

// probeTimeout bounds one liveness check of the DevTools endpoint. It is
// short because the usual answer arrives on loopback in microseconds; the
// wait that matters is launchTimeout below.
const probeTimeout = 500 * time.Millisecond

// launchTimeout is how long a freshly started Chrome is given to open its
// port before we give up on it.
const launchTimeout = 20 * time.Second

// ensureRemoteBrowser makes the DevTools endpoint at opts.RemoteURL
// answer, launching a browser against it if nothing does yet, and returns
// once it does.
func ensureRemoteBrowser(opts Options) error {
	if remoteAlive(opts.RemoteURL) {
		return nil
	}

	port, err := remotePort(opts.RemoteURL)
	if err != nil {
		return err
	}

	exe := opts.ExecutablePath
	if exe == "" {
		if exe = findChrome(); exe == "" {
			return fmt.Errorf("no Chrome or Chromium found to open %s with; set tools.browser.executable_path", opts.RemoteURL)
		}
	}

	// A profile directory is not optional here: Chrome ignores the
	// debugging port when it is running on the default one, so a browser
	// launched without this flag would come up and never answer.
	dir := opts.UserDataDir
	if dir == "" {
		dir = defaultProfileDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create browser profile directory: %w", err)
	}

	profile := ""
	if opts.UseRealProfile {
		var err error
		if profile, err = snapshotRealProfile(dir, opts.RealProfilePin); err != nil {
			return err
		}
	}

	args := []string{
		"--remote-debugging-port=" + port,
		"--user-data-dir=" + dir,
		// Without a first tab Chrome has no page to attach to on some
		// platforms, and the first-run interstitials would otherwise be
		// the first thing the agent sees.
		"--no-first-run",
		"--no-default-browser-check",
		"about:blank",
	}
	if profile != "" {
		args = append([]string{"--profile-directory=" + profile}, args...)
	}
	if opts.Headless {
		args = append([]string{"--headless=new"}, args...)
	}

	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch browser for %s: %w", opts.RemoteURL, err)
	}
	// The browser is deliberately not tied to this process: it holds the
	// user's signed-in profile, and closing Atlas should not sign them
	// out of everything or throw away the tabs they were looking at.
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(launchTimeout)
	for time.Now().Before(deadline) {
		if remoteAlive(opts.RemoteURL) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("browser started but %s never answered; another Chrome may already hold %s", opts.RemoteURL, dir)
}

// remoteAlive reports whether something is serving the DevTools protocol
// at raw.
func remoteAlive(raw string) bool {
	endpoint, err := url.JoinPath(raw, "json", "version")
	if err != nil {
		return false
	}
	client := http.Client{Timeout: probeTimeout}
	resp, err := client.Get(endpoint)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// remotePort pulls the port out of a DevTools URL, which is what Chrome
// wants on the command line.
func remotePort(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse remote_url %q: %w", raw, err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil || port == "" {
		return "", fmt.Errorf("remote_url %q needs an explicit port, for example http://127.0.0.1:9222", raw)
	}
	return port, nil
}

// defaultProfileDir is where a browser launched for attaching keeps its
// profile when the user named none. It sits under the user's cache dir so
// the logins written into it outlive any one session.
func defaultProfileDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "atlas-agent", "browser-profile")
}

// findChrome returns the first Chrome or Chromium install it can see, or
// the empty string. chromedp does this internally for the browsers it
// launches itself, but not for one we start ourselves.
func findChrome() string {
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}

	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, root := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LOCALAPPDATA"),
		} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(root, "Chromium", "Application", "chrome.exe"),
			)
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		candidates = []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/snap/bin/chromium",
		}
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
