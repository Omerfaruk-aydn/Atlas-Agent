			continue
		}
		if h := web.Handlers["/"]; h != nil && sameProxy(h.Proxy, target) {
			return id, true
		}
	}
	return "", false
}

// hasSession reports whether a foreground session id still exists.
func (sc *serveConfig) hasSession(id string) bool {
	_, ok := sc.Foreground[id]
	return ok
}

// sameProxy compares the configured proxy with Atlas' target. The CLI
// stores the expanded form, such as http://127.0.0.1:41234.
func sameProxy(got, want string) bool {
	return strings.TrimSuffix(got, "/") == strings.TrimSuffix(want, "/")
}
