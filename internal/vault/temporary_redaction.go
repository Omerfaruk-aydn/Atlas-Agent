package vault

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// Temporary redaction retains keyed fingerprints, never credential values.
// Fingerprints continue protecting later observations after a fill expires.
var temporaryRedaction = struct {
	sync.RWMutex
	key      [32]byte
	entries  map[string]map[int]map[[32]byte]struct{}
	byLength map[int]map[[32]byte]struct{}
}{key: newRedactionKey(), entries: make(map[string]map[int]map[[32]byte]struct{}), byLength: make(map[int]map[[32]byte]struct{})}

func newRedactionKey() [32]byte {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic("Cannot initialize credential redaction key")
	}
	return key
}

func temporaryDigest(value string) [32]byte {
	m := hmac.New(sha256.New, temporaryRedaction.key[:])
	_, _ = m.Write([]byte(value))
	var sum [32]byte
	copy(sum[:], m.Sum(nil))
	return sum
}

func registerTemporary(session, value string) error {
	encoded, _ := json.Marshal(value)
	forms := []string{value, string(encoded[1 : len(encoded)-1])}
	temporaryRedaction.Lock()
	defer temporaryRedaction.Unlock()
	if temporaryRedaction.entries[session] == nil {
		if len(temporaryRedaction.entries) >= 4096 {
			return errors.New("credential protection budget exhausted")
		}
		temporaryRedaction.entries[session] = make(map[int]map[[32]byte]struct{})
	}
	entries := temporaryRedaction.entries[session]
	count := 0
	for _, hashes := range entries {
		count += len(hashes)
	}
	for _, form := range forms {
		sum := temporaryDigest(form)
		if _, exists := entries[len(form)][sum]; exists {
			continue
		}
		if count >= 256 {
			return errors.New("credential protection budget exhausted")
		}
		if entries[len(form)] == nil {
			entries[len(form)] = make(map[[32]byte]struct{})
		}
		entries[len(form)][sum] = struct{}{}
		if temporaryRedaction.byLength[len(form)] == nil {
			temporaryRedaction.byLength[len(form)] = make(map[[32]byte]struct{})
		}
		temporaryRedaction.byLength[len(form)][sum] = struct{}{}
		count++
	}
	return nil
}

func redactTemporary(value string) string {
	temporaryRedaction.RLock()
	defer temporaryRedaction.RUnlock()
	// Match the original input in one pass, so replacements cannot expose
	// overlapping credentials or accidentally change later matches.
	var out strings.Builder
	m := hmac.New(sha256.New, temporaryRedaction.key[:])
	var digest [32]byte
	last := 0
	for i := 0; i < len(value); {
		matched := 0
		for n, hashes := range temporaryRedaction.byLength {
			if n == 0 || i+n > len(value) || n <= matched {
				continue
			}
			m.Reset()
			_, _ = m.Write([]byte(value[i : i+n]))
			copy(digest[:], m.Sum(digest[:0]))
			if _, ok := hashes[digest]; ok {
				matched = n
			}
		}
		if matched == 0 {
			i++
			continue
		}
		out.WriteString(value[last:i])
		out.WriteString("[REDACTED]")
		i += matched
		last = i
	}
	if last == 0 {
		return value
	}
	out.WriteString(value[last:])
	return out.String()
}
