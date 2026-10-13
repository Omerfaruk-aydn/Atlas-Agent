		}
		value = v
	}
	return value
}

// sourceInstructionAllows requires a real human request to use credentials
// from this source. Page text and model arguments never grant permission.
func sourceInstructionAllows(instruction, text, currentURL string) bool {
	p := normalizedSourceName(instruction)
	hasAny := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(p, word) {
				return true
			}
		}
		return false
	}
	if !hasAny("sifre", "parola", "password", "credential") || !hasAny("mail", "mesaj", "e-posta", "email", "document", "dosya") || !hasAny("gir", "doldur", "login", "sign in", "fill", "use", "kullan", "al") || hasAny("kullanma", "girme", "doldurma", "do not", "don't", "only read", "yalnizca oku") {
		// The current turn may not repeat "şifre" (e.g. "okul mailime giriş
		// yap"). When the text itself carries labeled credentials and the
		// instruction names a destination to use them at, accept.
		if !hasAny("kullanma", "girme", "doldurma", "do not", "don't", "only read", "yalnizca oku") &&
			sourcePasswordLine.MatchString(text) && sourceAccountLine.MatchString(text) &&
			hasAny("gir", "doldur", "login", "sign in", "fill", "use", "kullan", "al", "gec", "geç", "yap") {
			// fall through to destination checks below
		} else {
			return false
		}
	}
	u, err := url.Parse(currentURL)
	if err != nil || u.User != nil {
		return false
	}
	if _, err := vault.CredentialOrigin(u.Scheme + "://" + u.Host); err != nil {
		return false
	}
	// An explicitly named source URL authorizes that exact source host.
	for _, raw := range sourceLinks.FindAllString(instruction, -1) {
		expected, err := url.Parse(raw)
		if err == nil && expected.Host == u.Host && expected.Scheme == u.Scheme && (expected.Path == "" || expected.Path == "/" || expected.Path == u.Path) && (expected.RawQuery == "" || expected.RawQuery == u.RawQuery) && (expected.Fragment == "" || expected.Fragment == u.Fragment) {
			return true
		}
	}
	mailHost := u.Hostname() == "mail.google.com" || u.Hostname() == "outlook.live.com" || u.Hostname() == "outlook.office.com" || u.Hostname() == "outlook.office365.com"
	if !mailHost {
		return false
	}
	// Sender-name match authorizes. When the instruction does not name a
	// specific sender, a clear source+credential+action request on a mail
	// host is enough. When the named sender is a person but the message
	// comes from a system address (ois@topkapi.edu.tr), the human pointed
	// at a mailbox holding a reminder, not at another person's account.
	senderNamed := sourceSender.MatchString(instruction) || sourceFrom.MatchString(instruction)
	if !senderNamed && hasSourceCredentialInstruction(instruction) {
		return true
	}
	// The tool read may include Gmail UI chrome ("Yazdır", "Yeni pencerede")
	// before the real sender line. Scan the first few lines for a sender.
	senderLine := extractSenderLine(text)
	senderNorm := normalizedSourceName(senderLine)
	for _, re := range []*regexp.Regexp{sourceSender, sourceFrom} {
		match := re.FindStringSubmatch(instruction)
		if len(match) > 1 && len([]rune(match[1])) >= 3 && regexp.MustCompile(`(?:^|[^\pL\pN])`+regexp.QuoteMeta(normalizedSourceName(match[1]))+`(?:$|[^\pL\pN])`).MatchString(senderNorm) {
			return true
		}
	}
	// Named sender did not match. Accept only when the sender is a system
	// address (not a person) and the instruction clearly authorizes using
	// a credential from a named source at a named destination.
	if senderNamed && hasSourceCredentialInstruction(instruction) && isSystemSender(senderNorm) {
		return true
	}
	return false
}

// extractSenderLine scans the first few lines of a mail read for the
// actual sender. Gmail UI chrome ("Yazdır", "Yeni pencerede") sits above
// the header, so the first line is not reliable.
func extractSenderLine(text string) string {
	lines := strings.SplitN(text, "\n", 12)
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		// Skip Gmail chrome labels.
		lower := normalizedSourceName(l)
		if lower == "yazdir" || lower == "yeni pencerede" || lower == "gelen kutusu" || lower == "tumunu sec" || lower == "sil" || lower == "arsivle" || lower == "spam olarak isaretle" || lower == "diger" || lower == "etiketle" {
			continue
		}
		// An email address or a short name line is the sender.
		if strings.Contains(l, "@") || len([]rune(l)) < 80 {
			return l
		}
	}
	// Fallback: first email-looking token anywhere in the first 500 chars.
	if m := regexp.MustCompile(`[\w.+-]+@[\w.-]+\.\w+`).FindString(text[:min(len(text), 500)]); m != "" {
		return m
	}
	return strings.TrimSpace(lines[0])
}

// isSystemSender reports whether a mail header looks like an automated
// system address rather than a person's name.
func isSystemSender(header string) bool {
	h := strings.TrimSpace(strings.ToLower(header))
	if strings.Contains(h, "@") {
		return true
	}
	systemWords := []string{"ois", "otomasyon", "sistem", "system", "no-reply", "noreply", "bilgi", "info", "admin", "destek", "support", "ogrenci", "öğrenci", "isleri", "işleri"}
	for _, w := range systemWords {
		if strings.Contains(h, w) {
			return true
		}
	}
	return false
}

// sourceDestination uses the human's explicit URL when present. For a
// named service, a link in the selected source must match at least two
// meaningful host labels in the human instruction (e.g. OIS and Topkapi).
// The source itself cannot redirect the credential to an unrelated host.
func sourceDestination(instruction, text, currentURL string) string {
	source, _ := url.Parse(currentURL)
	origins := make(map[string]bool)
	for _, raw := range sourceLinks.FindAllString(instruction, -1) {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || source != nil && u.Host == source.Host {
			continue
		}
		if origin := sourceLinkOrigin(u); origin != "" {
			origins[origin] = true
		}
	}
	if len(origins) == 0 {
		p := " " + normalizedSourceName(instruction) + " "
		for _, raw := range sourceLinks.FindAllString(text, -1) {
			u, err := url.Parse(strings.TrimRight(raw, ".,);"))
			if err != nil || u.User != nil || source != nil && u.Host == source.Host {
				continue
			}
			matches := 0
			unmatched := false
			for _, label := range strings.Split(strings.ToLower(u.Hostname()), ".") {
				if len(label) < 3 || label == "www" || label == "com" || label == "edu" || label == "net" || label == "org" {
					continue
				}
				if regexp.MustCompile(`(?:^|[^a-z0-9])` + regexp.QuoteMeta(label) + `(?:$|[^a-z0-9])`).MatchString(p) {
					matches++
				} else {
					unmatched = true
				}
			}
			if matches >= 2 && !unmatched {
				if origin := sourceLinkOrigin(u); origin != "" {
					origins[origin] = true
				}
			}
		}
	}
	// The text may be truncated before the service link (common in short
	// tool reads). When the instruction names a service whose labels match
	// the sender's mail domain, accept that destination.
	if len(origins) == 0 {
		if fallback := destinationFromSenderDomain(instruction, text, source); fallback != "" {
			return fallback
		}
	}
	if len(origins) != 1 {
		return ""
	}
	for origin := range origins {
		return origin
	}
	return ""
}

// destinationFromSenderDomain derives the destination from the sender's
// mail domain when the tool read truncated the message before its link.
// "ois@topkapi.edu.tr" + instruction "OİS Topkapı" -> https://ois.topkapi.edu.tr.
func destinationFromSenderDomain(instruction, text string, source *url.URL) string {
	p := " " + normalizedSourceName(instruction) + " "
	// Take the first email-looking token in the header area as the sender.
	header := strings.SplitN(strings.TrimSpace(text), "\n", 3)
	sender := ""
	for _, line := range header {
		if strings.Contains(line, "@") {
			sender = strings.TrimSpace(line)
			break
		}
	}
	if sender == "" {
		return ""
	}
	at := strings.LastIndex(sender, "@")
	if at < 0 || at+1 >= len(sender) {
		return ""
	}
	domain := strings.TrimRight(sender[at+1:], " .,;)>\"'")
	if domain == "" || strings.Contains(domain, "/") {
		return ""
	}
	labels := strings.Split(strings.ToLower(domain), ".")
	if len(labels) < 2 {
		return ""
	}
	matches := 0
	for _, label := range labels {
		if len(label) < 3 || label == "www" || label == "com" || label == "edu" || label == "net" || label == "org" {
			continue
		}
		if regexp.MustCompile(`(?:^|[^a-z0-9])` + regexp.QuoteMeta(label) + `(?:$|[^a-z0-9])`).MatchString(p) {
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
