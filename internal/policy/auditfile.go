package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// appendFile writes one record as a JSON line. Failures are ignored: the
// audit log must never stop a command from being judged.
func (a *Audit) appendFile(e Entry) {
	if a.file == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.file), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(a.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}
