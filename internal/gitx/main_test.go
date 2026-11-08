package gitx

import (
	"os"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.KeepGitInsideTemp()
	os.Exit(m.Run())
}
