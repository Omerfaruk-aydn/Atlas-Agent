package isolation

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

// browserLaunchScript runs inside the sandbox. It waits for Explorer, then
// keeps one browser window on screen. Chrome is not started with
// --no-sandbox: that flag is what paints the unsupported-flag banner, and
// the desktop is already up by the time the browser launches. A missing
// Chrome falls back to the Edge that ships with the sandbox image.
func browserLaunchScript() string {
	return strings.ReplaceAll(`@echo off
start "" C:\AtlasBin\atlas.exe node serve --isolated-exchange C:\AtlasExchange
ping -n 8 127.0.0.1 >nul
call :browser
ping -n 8 127.0.0.1 >nul
tasklist /FI "IMAGENAME eq chrome.exe" | find /I "chrome.exe" >nul && goto :eof
tasklist /FI "IMAGENAME eq msedge.exe" | find /I "msedge.exe" >nul && goto :eof
call :browser
goto :eof
:browser
if exist C:\Chromium\chrome.exe (
  start "" C:\Chromium\chrome.exe --user-data-dir=C:\ChromeProfile --no-first-run --disable-sync --no-default-browser-check --disable-gpu --hide-crash-restore-bubble --start-maximized --new-window about:blank
  goto :eof
)
if exist "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe" (
  start "" "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe" --user-data-dir=C:\ChromeProfile\edge --no-first-run --disable-sync --no-default-browser-check --disable-gpu --hide-crash-restore-bubble --start-maximized --new-window about:blank
)
goto :eof
`, "\n", "\r\n")
}

// DefaultChrome is the host Chrome directory and a profile folder that
// survives the sandbox being closed. An empty directory means no browser
// was found.
func DefaultChrome() (dir, profile string) {
	exe := browser.FindChrome()
	if exe == "" {
		return "", ""
	}
	profile = filepath.Join(config.GlobalDevicesDir(), "sandbox-chrome")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return "", ""
	}
	return filepath.Dir(exe), profile
}
