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
