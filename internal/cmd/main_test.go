package cmd

import (
	"os"
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

	code := m.Run()

	// m.Run's result has to be handed to os.Exit, which runs no
	// deferred functions, so the cleanup goes here.
	os.RemoveAll(dir)
	os.Exit(code)
}
