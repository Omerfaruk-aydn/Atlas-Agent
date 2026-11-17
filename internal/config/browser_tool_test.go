package config

import (
	"testing"
	"time"
)

func TestToolBrowserDefaultsToEnabledAndCanBeSwitchedOff(t *testing.T) {
	var b ToolBrowser
	if !b.IsEnabled() {
		t.Fatal("IsEnabled() = false, want true when unset")
	}
	off := false
	if (ToolBrowser{Enabled: &off}).IsEnabled() {
		t.Fatal("IsEnabled() = true, want false when enabled is false")
	}
}

func TestToolBrowserEnabledHonorsExplicitTrue(t *testing.T) {
	enabled := true
	b := ToolBrowser{Enabled: &enabled}
	if !b.IsEnabled() {
		t.Fatal("IsEnabled() = false, want true")
	}
}

func TestToolBrowserDefaultsToHeadless(t *testing.T) {
	var b ToolBrowser
	if !b.IsHeadless() {
		t.Fatal("IsHeadless() = false, want true when unset")
	}
}

func TestToolBrowserHeadlessHonorsExplicitFalse(t *testing.T) {
	headless := false
	b := ToolBrowser{Headless: &headless}
	if b.IsHeadless() {
		t.Fatal("IsHeadless() = true, want false")
	}
}

func TestToolBrowserActionTimeoutDefaultsTo30Seconds(t *testing.T) {
	var b ToolBrowser
	if got := b.GetActionTimeout(); got != 30*time.Second {
		t.Fatalf("GetActionTimeout() = %v, want 30s", got)
	}
}

func TestToolBrowserActionTimeoutHonorsConfiguredValue(t *testing.T) {
	d := 90 * time.Second
	b := ToolBrowser{ActionTimeout: &d}
	if got := b.GetActionTimeout(); got != d {
		t.Fatalf("GetActionTimeout() = %v, want %v", got, d)
	}
}

func TestToolBrowserIdleTimeoutDefaultsTo10Minutes(t *testing.T) {
	var b ToolBrowser
	if got := b.GetIdleTimeout(); got != 10*time.Minute {
		t.Fatalf("GetIdleTimeout() = %v, want 10m", got)
	}
}

func TestToolBrowserIdleTimeoutHonorsConfiguredValue(t *testing.T) {
	d := 2 * time.Minute
	b := ToolBrowser{IdleTimeout: &d}
	if got := b.GetIdleTimeout(); got != d {
		t.Fatalf("GetIdleTimeout() = %v, want %v", got, d)
	}
}
