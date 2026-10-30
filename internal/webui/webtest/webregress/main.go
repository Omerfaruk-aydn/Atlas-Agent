// Command webregress runs one deterministic Atlas browser-use scenario.
// Browser/profile/artifact directories must be supplied by the isolated test
// harness; this command never selects or copies a real user profile.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webui/webtest"
)

func main() {
	os.Exit(run())
}

func run() int {
	flags := flag.NewFlagSet("webregress", flag.ContinueOnError)
	scenarioPath := flags.String("scenario", "", "JSON scenario path")
	artifactDir := flags.String("artifacts", "", "isolated artifact directory")
	profileDir := flags.String("profile", "", "new isolated browser profile directory")
	workingDir := flags.String("working-dir", "", "isolated interaction state directory")
	chromium := flags.String("chromium", "", "supported Chromium executable")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if *scenarioPath == "" || *artifactDir == "" || *profileDir == "" || *workingDir == "" || *chromium == "" {
		fmt.Fprintln(os.Stderr, "scenario, artifacts, profile, working-dir and chromium are required")
		return 2
	}
	info, err := os.Stat(*chromium)
	if err != nil || info.IsDir() {
		fmt.Fprintln(os.Stderr, "supported Chromium executable is unavailable")
		return 2
	}
	data, err := os.ReadFile(*scenarioPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var scenario webtest.BrowserRegressionScenario
	if err := json.Unmarshal(data, &scenario); err != nil || scenario.Name == "" {
		fmt.Fprintln(os.Stderr, "invalid browser regression scenario")
		return 2
	}
	if err := os.MkdirAll(*profileDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := os.MkdirAll(*workingDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	manager := browser.NewManager(browser.Options{
		ExecutablePath: *chromium,
		Headless:       true,
		UserDataDir:    *profileDir,
		UseRealProfile: false,
		ActionTimeout:  15 * time.Second,
		IdleTimeout:    30 * time.Second,
	})
	defer manager.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := webtest.RunBrowserRegression(ctx, manager, *workingDir, *artifactDir, scenario)
	redact := func(value string) string { return vault.Redact("web-regression", value) }
	if err := webtest.WriteBrowserRegressionJUnit(filepath.Join(*artifactDir, "junit.xml"), report, redact); err != nil {
		fmt.Fprintln(os.Stderr, "JUnit report failed:", err)
		return 1
	}
	encoded, _ := json.Marshal(report)
	fmt.Println(redact(string(encoded)))
	if !report.Passed {
		return 1
	}
	return 0
}
