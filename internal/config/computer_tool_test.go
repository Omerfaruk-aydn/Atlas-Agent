package config

import (
	"testing"
	"time"
)

func TestToolComputerDefaultsToDisabled(t *testing.T) {
	var c ToolComputer
	if c.IsEnabled() {
		t.Fatal("IsEnabled() = true, want false when unset")
	}
}

func TestToolComputerEnabledHonorsExplicitTrue(t *testing.T) {
	enabled := true
	c := ToolComputer{Enabled: &enabled}
	if !c.IsEnabled() {
		t.Fatal("IsEnabled() = false, want true")
	}
}

func TestToolComputerEnabledHonorsExplicitFalse(t *testing.T) {
	enabled := false
	c := ToolComputer{Enabled: &enabled}
	if c.IsEnabled() {
		t.Fatal("IsEnabled() = true, want false")
	}
}

func TestToolComputerActionTimeoutDefaultsTo30Seconds(t *testing.T) {
	var c ToolComputer
	if got := c.GetActionTimeout(); got != 30*time.Second {
		t.Fatalf("GetActionTimeout() = %v, want 30s", got)
	}
}

func TestToolComputerActionTimeoutHonorsConfiguredValue(t *testing.T) {
	d := 90 * time.Second
	c := ToolComputer{ActionTimeout: &d}
	if got := c.GetActionTimeout(); got != d {
		t.Fatalf("GetActionTimeout() = %v, want %v", got, d)
	}
}
