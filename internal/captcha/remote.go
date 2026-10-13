			"type": "ImageToTextTask",
			"body": base64.StdEncoding.EncodeToString(image),
		}, nil
	case KindRecaptchaV2:
		t := "ReCaptchaV2TaskProxyless"
		if r.Provider == RemoteAntiCaptcha {
			t = "NoCaptchaTaskProxyless"
		}
		return map[string]any{
			"type":       t,
			"websiteURL": ch.PageURL,
			"websiteKey": ch.SiteKey,
		}, nil
	case KindRecaptchaV3:
		t := "ReCaptchaV3TaskProxyless"
		if r.Provider == RemoteAntiCaptcha {
			t = "RecaptchaV3TaskProxyless"
		}
		return map[string]any{
			"type":       t,
			"websiteURL": ch.PageURL,
			"websiteKey": ch.SiteKey,
			"pageAction": firstNonEmpty(ch.Action, "verify"),
		}, nil
	case KindHCaptcha:
		t := "HCaptchaTaskProxyless"
		if r.Provider == RemoteAntiCaptcha {
			t = "HCaptchaTaskProxyless"
		}
		return map[string]any{
			"type":       t,
			"websiteURL": ch.PageURL,
			"websiteKey": ch.SiteKey,
		}, nil
	case KindTurnstile:
		return map[string]any{
			"type":       "AntiTurnstileTaskProxyless",
			"websiteURL": ch.PageURL,
			"websiteKey": ch.SiteKey,
		}, nil
	default:
		return nil, fmt.Errorf("captcha: remote provider %s does not support kind %s", r.Provider, ch.Kind)
	}
}

func (r *RemoteSolver) postJSON(ctx context.Context, endpoint string, body map[string]any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captcha: remote request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("captcha: remote endpoint status %d", resp.StatusCode)
	}
	return raw, nil
}
