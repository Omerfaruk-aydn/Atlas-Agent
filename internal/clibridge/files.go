package clibridge

import (
	"embed"
	"encoding/json"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func marshalIndent(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func atomicWrite(path string, data []byte) error { return engineering.AtomicWrite(path, data) }

func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o700) }

//go:embed examples/*.json
var examples embed.FS

// Examples returns the bundled example profiles by name. They are inert
// documents: nothing is installed or run until a user imports one.
func Examples() map[string][]byte {
	out := map[string][]byte{}
	entries, _ := examples.ReadDir("examples")
	for _, entry := range entries {
		data, err := examples.ReadFile(path.Join("examples", entry.Name()))
		if err == nil {
			out[strings.TrimSuffix(entry.Name(), ".json")] = data
		}
	}
	return out
}

// ExampleNames lists bundled examples in order.
func ExampleNames() []string {
	var names []string
	for name := range Examples() {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
