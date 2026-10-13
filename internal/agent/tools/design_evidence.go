func designEvidenceKey(root, id string) string {
	path, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path + ":" + id
}

func registerDesignAudit(root string, audit designAudit) {
	data, _ := json.Marshal(audit)
	key := designEvidenceKey(root, audit.ID)
	observedDesignAudits.Lock()
	defer observedDesignAudits.Unlock()
	if _, exists := observedDesignAudits.hashes[key]; !exists {
		if len(observedDesignAudits.keys) >= 256 {
			delete(observedDesignAudits.hashes, observedDesignAudits.keys[0])
			observedDesignAudits.keys = observedDesignAudits.keys[1:]
		}
		observedDesignAudits.keys = append(observedDesignAudits.keys, key)
	}
	observedDesignAudits.hashes[key] = imageDeliveryHash(data)
}

func designAuditWasObserved(root string, audit designAudit) bool {
	data, _ := json.Marshal(audit)
	observedDesignAudits.Lock()
	defer observedDesignAudits.Unlock()
	return observedDesignAudits.hashes[designEvidenceKey(root, audit.ID)] == imageDeliveryHash(data)
}
