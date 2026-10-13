	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return nil, fmt.Errorf("artifact must be a regular file of at most %d bytes", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("artifact grew beyond %d bytes while reading", maxBytes)
	}
	return data, nil
}

func designUserApproved(ctx context.Context) bool {
	text, _ := ctx.Value(userInstructionKey{}).(string)
	text = strings.ToLower(strings.TrimSpace(text))
	for _, prefix := range []string{"tasarımı onaylıyorum", "bu tasarımı onaylıyorum", "bu tasarımı uygula", "evet, bu tasarımı", "approve this design", "i approve this design", "apply this design"} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func designUserReset(ctx context.Context) bool {
	text, _ := ctx.Value(userInstructionKey{}).(string)
	text = strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(text, "tasarım belleğini sıfırla") || strings.HasPrefix(text, "reset the design memory")
}
