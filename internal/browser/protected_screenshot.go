		identities := make(map[string]RefIdentity, len(elements))
		for i := range elements {
			identities[elements[i].Ref] = RefIdentity{Role: elements[i].Role, Name: elements[i].Name}
			elements[i].Name = redact(elements[i].Name)
			elements[i].Value = redact(elements[i].Value)
		}
		if !full {
			visible := elements[:0]
			for _, element := range elements {
				if element.Visible {
					visible = append(visible, element)
				}
			}
			elements = visible
		}
		var refs map[string]RefIdentity
		data, refs, err = annotateScreenshotPNG(data, elements, scale, MaxScreenshotWidth)
		if err != nil || identity != s.ExecutionIdentity(ctx) {
			return nil, 0, errors.New("credential_redaction_stale_annotation")
		}
		// Display redaction must not change the private DOM identity used
		// to validate a ref before input. These names are not image labels.
		for ref := range refs {
			refs[ref] = identities[ref]
		}
		s.mu.Lock()
		s.screenshotIdentity, s.screenshotReferences = identity, refs
		s.mu.Unlock()
	}
	return data, masked, nil
}
