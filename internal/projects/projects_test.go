package projects

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegisterAndList(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Override the projects file path for testing
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(tmpDir, "Atlas-Agent"))

	// Test registering a project
	err := Register("/home/user/project1", "/home/user/project1/.atlas")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// List projects
	projects, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("Expected 1 project, got %d", len(projects))
	}

	if projects[0].Path != "/home/user/project1" {
		t.Errorf("Expected path /home/user/project1, got %s", projects[0].Path)
	}

	if projects[0].DataDir != "/home/user/project1/.atlas" {
		t.Errorf("Expected data_dir /home/user/project1/.atlas, got %s", projects[0].DataDir)
	}

	// Register another project
	err = Register("/home/user/project2", "/home/user/project2/.atlas")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	projects, err = List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("Expected 2 projects, got %d", len(projects))
	}

	// Most recent should be first
	if projects[0].Path != "/home/user/project2" {
		t.Errorf("Expected most recent project first, got %s", projects[0].Path)
	}
}

func TestRegistrationRecencySurvivesClockTiesAndRollback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(dir, "Atlas-Agent"))
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, registerAt("project1", "data1", now))
	require.NoError(t, registerAt("project2", "data2", now))
	items, err := List()
	require.NoError(t, err)
	require.Equal(t, "project2", items[0].Path)
	require.True(t, items[0].LastAccessed.After(items[1].LastAccessed))
	require.NoError(t, registerAt("project1", "updated", now.Add(-time.Hour)))
	items, err = List()
	require.NoError(t, err)
	require.Equal(t, "project1", items[0].Path)
	require.Equal(t, "updated", items[0].DataDir)
	require.True(t, items[0].LastAccessed.After(items[1].LastAccessed))
}

func TestRegisterUpdatesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(tmpDir, "Atlas-Agent"))

	// Register a project
	err := Register("/home/user/project1", "/home/user/project1/.atlas")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	projects, _ := List()
	firstAccess := projects[0].LastAccessed

	// Wait a bit and re-register
	time.Sleep(10 * time.Millisecond)

	err = Register("/home/user/project1", "/home/user/project1/.atlas-new")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	projects, _ = List()

	if len(projects) != 1 {
		t.Fatalf("Expected 1 project after update, got %d", len(projects))
	}

	if projects[0].DataDir != "/home/user/project1/.atlas-new" {
		t.Errorf("Expected updated data_dir, got %s", projects[0].DataDir)
	}

	if !projects[0].LastAccessed.After(firstAccess) {
		t.Error("Expected LastAccessed to be updated")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(tmpDir, "Atlas-Agent"))

	// List before any projects exist
	projects, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(projects) != 0 {
		t.Errorf("Expected 0 projects, got %d", len(projects))
	}
}

func TestProjectsFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(tmpDir, "Atlas-Agent"))

	expected := filepath.Join(tmpDir, "Atlas-Agent", "projects.json")
	actual := projectsFilePath()

	if actual != expected {
		t.Errorf("Expected %s, got %s", expected, actual)
	}
}

func TestRegisterWithParentDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(tmpDir, "Atlas-Agent"))

	// Register a project where .atlas is in a parent directory.
	// e.g., working in /home/user/monorepo/packages/app but .atlas is at /home/user/monorepo/.atlas
	err := Register("/home/user/monorepo/packages/app", "/home/user/monorepo/.atlas")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	projects, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("Expected 1 project, got %d", len(projects))
	}

	if projects[0].Path != "/home/user/monorepo/packages/app" {
		t.Errorf("Expected path /home/user/monorepo/packages/app, got %s", projects[0].Path)
	}

	if projects[0].DataDir != "/home/user/monorepo/.atlas" {
		t.Errorf("Expected data_dir /home/user/monorepo/.atlas, got %s", projects[0].DataDir)
	}
}

func TestRegisterWithExternalDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(tmpDir, "Atlas-Agent"))

	// Register a project where .atlas is in a completely different location.
	// e.g., project at /home/user/project but data stored at /var/data/Atlas-Agent/myproject
	err := Register("/home/user/project", "/var/data/Atlas-Agent/myproject")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	projects, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("Expected 1 project, got %d", len(projects))
	}

	if projects[0].Path != "/home/user/project" {
		t.Errorf("Expected path /home/user/project, got %s", projects[0].Path)
	}

	if projects[0].DataDir != "/var/data/Atlas-Agent/myproject" {
		t.Errorf("Expected data_dir /var/data/Atlas-Agent/myproject, got %s", projects[0].DataDir)
	}
}
