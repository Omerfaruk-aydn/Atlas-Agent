package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
)

// TestMain points this package's tests at an empty global configuration.
//
// config.Init reads the user's own atlas.json and its data-directory
// sibling wherever it finds them, and nothing about passing t.TempDir()
// as the working and data directories stops that. So without this, the
// configuration of whoever is running the tests decides what they see:
// a model role they happen to have set makes a subagent that is supposed
// to read as unresolved resolve instead, and a list asserted to be empty
// comes back holding their roles. Those tests then pass on CI, which has
// no configuration, and fail only on the machine that wrote them -- the
// least useful way round.
//
// GlobalSubagentsDirs defaults to the real ~/.config/atlas/agents
// regardless of GLOBAL_CONFIG/GLOBAL_DATA -- it is a separate default,
// keyed off home.Config() rather than the app's own data directory --
// so a subagent saved there for real (through the "new subagent" dialog,
// or atlas_config's save_subagent) is just as real a leak into "with
// nothing authored" counts as a stray model role would be. Isolating it
// here, the same way, means a subagent that exists on the machine
// running the tests cannot change what these tests see.
//
// internal/config's own tests already isolate this way, one t.Setenv at
// a time. Doing it once for the package covers the tests that exist and
// the ones added later, which is the point: the leak is a property of
// calling config.Init at all, not of any one test.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "atlas-cmd-config")
	if err != nil {
		panic(err)
	}
	os.Setenv(appenv.Prefix+"GLOBAL_CONFIG", dir)
	os.Setenv(appenv.Prefix+"GLOBAL_DATA", dir)
	os.Setenv(appenv.Prefix+"SUBAGENTS_DIR", filepath.Join(dir, "agents"))

	code := m.Run()

	// m.Run's result has to be handed to os.Exit, which runs no
	// deferred functions, so the cleanup goes here.
	os.RemoveAll(dir)
	os.Exit(code)
}
