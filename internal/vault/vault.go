// Package vault keeps origin-bound passwords outside model-visible state.
package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
)

type (
	Store struct {
		State     *agentstate.Store
		Namespace string
	}
	Item struct {
		ID       string `json:"id"`
		Origin   string `json:"origin"`
		Username string `json:"username"`
		Sealed   []byte `json:"sealed,omitempty"`
		Disabled bool   `json:"disabled,omitempty"`
	}
	secret struct {
		Origin   string `json:"origin"`
		Password string `json:"password"`
	}
)

func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", errors.New("vault requires an exact HTTPS origin")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("vault origin must not contain a path, query or fragment")
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimSuffix(host, ":443")
	return "https://" + host, nil
}

func (v *Store) Add(ctx context.Context, id, origin, username, password string) error {
	if v == nil || v.State == nil {
		return errors.New("vault unavailable")
	}
	normalized, err := Origin(origin)
	if err != nil {
		return err
	}
	if id == "" || len(id) > 128 || username == "" || len(username) > 512 || len(password) < 1 || len(password) > 4096 {
		return errors.New("invalid vault item")
	}
	plain, _ := json.Marshal(secret{Origin: normalized, Password: password})
	sealed, err := seal(plain)
	clear(plain)
	if err != nil {
		return err
	}
	return v.State.Put(ctx, v.Namespace, id, 0, Item{ID: id, Origin: normalized, Username: username, Sealed: sealed})
}

func (v *Store) List(ctx context.Context) ([]Item, error) {
	if v == nil || v.State == nil {
		return nil, errors.New("vault unavailable")
	}
	rows, err := v.State.List(ctx, v.Namespace)
	if err != nil {
		return nil, err
	}
	out := []Item{}
	for _, r := range rows {
		var item Item
		if err := json.Unmarshal(r.Payload, &item); err != nil {
			return nil, err
		}
		if !item.Disabled {
			item.Sealed = nil
			out = append(out, item)
		}
	}
	return out, nil
}

func (v *Store) Fill(ctx context.Context, id, origin, session string, inject func(string, string) error) error {
	var item Item
	if v == nil || v.State == nil {
		return errors.New("vault unavailable")
	}
	rev, err := v.State.Get(ctx, v.Namespace, id, &item)
	if err != nil {
		return err
	}
	if rev == 0 || item.Disabled {
		return errors.New("vault item not found")
	}
	normalized, err := Origin(origin)
	if err != nil || normalized != item.Origin {
		return errors.New("credential origin mismatch")
	}
	plain, err := unseal(item.Sealed)
	if err != nil {
		return errors.New("vault is locked or unreadable")
	}
	defer clear(plain)
	var value secret
	if json.Unmarshal(plain, &value) != nil || value.Origin != normalized {
		return errors.New("credential binding is invalid")
	}
	if err := Register(session, value.Password); err != nil {
		return err
	}
	if err := inject(normalized, value.Password); err != nil {
		return errors.New("credential fill failed; inspect the current page")
	}
	return nil
}

func (v *Store) Remove(ctx context.Context, id string) error {
	return agentstate.Update[Item](ctx, v.State, v.Namespace, id, func(i *Item) error {
		if i.ID == "" {
			return errors.New("vault item not found")
		}
		i.Sealed = nil
		i.Disabled = true
		return nil
	})
}

var redaction = struct {
	sync.RWMutex
	values map[string][]string
}{values: map[string][]string{}}

// Register protects later page observations and disables secret-session images.
func Register(session, value string) error {
	redaction.Lock()
	defer redaction.Unlock()
	if len(redaction.values[session]) >= 128 || len(redaction.values) >= 4096 {
		return errors.New("credential protection budget exhausted; restart before filling more secrets")
	}
	redaction.values[session] = append(redaction.values[session], value)
	return nil
}

func Sensitive(session string) bool {
	redaction.RLock()
	defer redaction.RUnlock()
	if session == "" {
		return len(redaction.values) > 0
	}
	return len(redaction.values[session]) > 0
}

func Redact(session, value string) string {
	redaction.RLock()
	defer redaction.RUnlock()
	for _, values := range redaction.values {
		for _, secret := range values {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[REDACTED]")
				encoded, _ := json.Marshal(secret)
				if len(encoded) > 2 {
					value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), "[REDACTED]")
				}
			}
		}
	}
	return value
}
