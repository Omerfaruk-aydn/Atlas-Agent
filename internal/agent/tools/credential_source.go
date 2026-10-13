			matches++
		}
	}
	if matches < 1 {
		return ""
	}
	if source != nil && source.Hostname() == domain {
		return ""
	}
	if origin, err := vault.CredentialOrigin("https://" + domain); err == nil {
		return origin
	}
	return ""
}

func sourceLinkOrigin(u *url.URL) string {
	if origin, err := vault.CredentialOrigin(u.Scheme + "://" + u.Host); err == nil {
		return origin
	}
	// Old mail may contain an HTTP link, but the actual password field
	// must be on its HTTPS destination before a credential can bind.
	if u.Scheme == "http" {
		if origin, err := vault.CredentialOrigin("https://" + u.Host); err == nil {
			return origin
		}
	}
	return ""
}

// importSourceCredential runs before browser text is returned or recorded.
// It transfers an authorized value internally, leaving only masked text and
// non-secret instructions for the model. Passwords found in source text are
// always masked, even when the import cannot complete.
func importSourceCredential(ctx context.Context, currentURL, text string) string {
	instruction, _ := ctx.Value(userInstructionKey{}).(string)
	// Always mask password-like values when the instruction references
	// credentials: a raw password must never reach the model or logs.
	masked := maskSourcePasswords(text)
	grant, _ := ctx.Value(sourceCredentialGrantKey{}).(*sourceCredentialGrant)
	if grant == nil || !sourceInstructionAllows(instruction, text, currentURL) {
		if masked != text && credentialSourceRead(ctx, "text") {
			return masked + "\n[credential_source_unresolved: select one message with labeled username/password and an unambiguous destination. Clarify only the source or destination if needed; do not ask the user to retype a password already in the authorized source.]"
		}
		return masked
	}
	sessionID := GetSessionFromContext(ctx)
	account := uniqueSourceValue(sourceAccountLine, text)
	secret := uniqueSourceValue(sourcePasswordLine, text)
	origin := sourceDestination(instruction, text, currentURL)
	if account == "" || secret == "" || origin == "" || len(account) > 320 || strings.ContainsAny(account, "\r\n\t ") {
		return masked + "\n[credential_source_unresolved: select one message with labeled username/password and an unambiguous destination. Clarify only the source or destination if needed; do not ask the user to retype a password already in the authorized source.]"
	}
	// Protect the selected password even if staging fails or was already used.
	id, _ := ctx.Value(userInstructionIDKey{}).(uint64)
	digest := sha256.Sum256([]byte(currentURL + "\x00" + origin + "\x00" + account))
	key := fmt.Sprintf("%d:%s", id, hex.EncodeToString(digest[:]))
	grant.mu.Lock()
	defer grant.mu.Unlock()
	if grant.used[key] {
		return vault.Redact(sessionID, masked) + "\n[credential_source_already_imported: this read does not re-arm a fill. Use the existing handle or observe the outcome; do not resubmit an uncertain fill.]"
	}
	value := []byte(secret)
	defer func() { clear(value) }()
	ready, err := vault.Ephemeral.SupplyFromSource(sessionID, key, origin, account, value)
	if err != nil {
		return masked + "\n[credential_source_unavailable: temporary storage refused the transfer; no password was sent to a page.]"
	}
	grant.used[key] = true
	if !ready {
		return vault.Redact(sessionID, masked) + "\n[credential_source_already_imported: no new credential was created.]"
	}
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
