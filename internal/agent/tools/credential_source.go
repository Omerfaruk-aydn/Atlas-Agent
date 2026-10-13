	return vault.Redact(sessionID, masked) + "\n[Atlas credential imported from the authorized source for account " + account + " at " + origin + ". Navigate there, observe the password field, call browser action credential_request with that account, then browser action vault_fill with the returned handle. No phone card or repeated password request is needed. These are browser actions, not separate tools. Fill the username as a replacement, not appended text.]"
}

// maskSourcePasswords replaces every labeled password value with
// [REDACTED] so a raw secret never reaches the model even when the
// credential import cannot complete.
func maskSourcePasswords(text string) string {
	out := text
	for _, match := range sourcePasswordLine.FindAllStringSubmatch(text, -1) {
		if value := strings.TrimSpace(match[1]); value != "" {
			out = strings.ReplaceAll(out, value, "[REDACTED]")
		}
	}
	return out
}
