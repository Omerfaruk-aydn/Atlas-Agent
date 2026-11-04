package dots

import (
	"fmt"
	"os"
	"time"
)

// writeAtomic replaces path with data through a temporary file, so a
// reader never sees half a file. On Windows the final rename fails for a
// moment while another process, such as a virus scanner or the search
// indexer, has the old file open; the rename is retried briefly before
// giving up.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	var err error
	for i, wait := 0, 5*time.Millisecond; i < 8; i, wait = i+1, wait*2 {
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
		time.Sleep(wait)
	}
	_ = os.Remove(tmp)
	return fmt.Errorf("replace %s: %w", path, err)
}
