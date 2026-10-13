	}
	if r.PollInterval <= 0 {
		r.PollInterval = 3 * time.Second
	}
	if r.Timeout <= 0 {
		r.Timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	switch r.Provider {
	case Remote2Captcha:
		return r.solve2Captcha(ctx, ch, image)
	case RemoteCapSolver, RemoteAntiCaptcha:
		return r.solveTaskAPI(ctx, ch, image)
	default:
		return Solution{}, fmt.Errorf("captcha: unknown remote provider %q", r.Provider)
	}
}

// --- 2captcha ---------------------------------------------------------

func (r *RemoteSolver) solve2Captcha(ctx context.Context, ch Challenge, image []byte) (Solution, error) {
	form := url.Values{}
	form.Set("key", r.APIKey)
	form.Set("json", "1")
	if r.SoftID != "" {
		form.Set("soft_id", r.SoftID)
	}
	switch ch.Kind {
	case KindImageText, KindMath:
		if len(image) == 0 {
			return Solution{}, fmt.Errorf("captcha: remote image task needs an image")
		}
		form.Set("method", "base64")
		form.Set("body", base64.StdEncoding.EncodeToString(image))
		if ch.Question != "" {
			form.Set("textinstructions", ch.Question)
		}
	case KindRecaptchaV2:
		form.Set("method", "userrecaptcha")
		form.Set("googlekey", ch.SiteKey)
		form.Set("pageurl", ch.PageURL)
	case KindRecaptchaV3:
		form.Set("method", "userrecaptcha")
		form.Set("googlekey", ch.SiteKey)
		form.Set("pageurl", ch.PageURL)
		form.Set("version", "v3")
		form.Set("action", firstNonEmpty(ch.Action, "verify"))
		form.Set("min_score", "0.3")
	case KindHCaptcha:
		form.Set("method", "hcaptcha")
		form.Set("sitekey", ch.SiteKey)
		form.Set("pageurl", ch.PageURL)
	case KindTurnstile:
		form.Set("method", "turnstile")
		form.Set("sitekey", ch.SiteKey)
		form.Set("pageurl", ch.PageURL)
	default:
		return Solution{}, fmt.Errorf("captcha: 2captcha does not support kind %s", ch.Kind)
	}

	submit, err := r.postForm(ctx, "https://2captcha.com/in.php", form)
	if err != nil {
		return Solution{}, err
	}
	if submit.Request == "" {
		return Solution{}, fmt.Errorf("captcha: 2captcha submit rejected: %s", submit.ErrorText)
	}
	for {
		select {
		case <-ctx.Done():
			return Solution{}, ctx.Err()
		case <-time.After(r.PollInterval):
		}
		q := url.Values{}
		q.Set("key", r.APIKey)
		q.Set("json", "1")
		q.Set("action", "get")
		q.Set("id", submit.Request)
		poll, err := r.postForm(ctx, "https://2captcha.com/res.php", q)
		if err != nil {
			return Solution{}, err
		}
		if poll.Request != "" {
			return Solution{Text: poll.Request, Token: poll.Request, Confidence: 0.9}, nil
		}
		if strings.EqualFold(poll.ErrorText, "CAPCHA_NOT_READY") || strings.EqualFold(poll.ErrorText, "CAPTCHA_NOT_READY") {
			continue
		}
		if poll.ErrorText != "" {
			return Solution{}, fmt.Errorf("captcha: 2captcha: %s", poll.ErrorText)
		}
	}
}

type twocaptchaResp struct {
	Status    int    `json:"status"`
	Request   string `json:"request"`
	ErrorText string `json:"error_text"`
}

func (r *RemoteSolver) postForm(ctx context.Context, endpoint string, form url.Values) (twocaptchaResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return twocaptchaResp{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := r.Client.Do(req)
	if err != nil {
		return twocaptchaResp{}, fmt.Errorf("captcha: 2captcha request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return twocaptchaResp{}, err
	}
	var out twocaptchaResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return twocaptchaResp{}, fmt.Errorf("captcha: 2captcha response: %w", err)
	}
	return out, nil
}

// --- CapSolver / Anti-Captcha task API ---------------------------------

func (r *RemoteSolver) solveTaskAPI(ctx context.Context, ch Challenge, image []byte) (Solution, error) {
	base := "https://api.capsolver.com"
	if r.Provider == RemoteAntiCaptcha {
		base = "https://api.anti-captcha.com"
	}
	task, err := r.taskPayload(ch, image)
	if err != nil {
		return Solution{}, err
	}
	create := map[string]any{
		"clientKey": r.APIKey,
		"task":      task,
	}
	raw, err := r.postJSON(ctx, base+"/createTask", create)
	if err != nil {
		return Solution{}, err
	}
	var created struct {
		ErrorID          int    `json:"errorId"`
		ErrorCode        string `json:"errorCode"`
		ErrorDescription string `json:"errorDescription"`
		TaskID           string `json:"taskId"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return Solution{}, fmt.Errorf("captcha: createTask decode: %w", err)
	}
	if created.ErrorID != 0 || created.TaskID == "" {
		return Solution{}, fmt.Errorf("captcha: createTask failed: %s %s", created.ErrorCode, created.ErrorDescription)
	}
	for {
		select {
		case <-ctx.Done():
			return Solution{}, ctx.Err()
		case <-time.After(r.PollInterval):
		}
		raw, err := r.postJSON(ctx, base+"/getTaskResult", map[string]any{
			"clientKey": r.APIKey,
			"taskId":    created.TaskID,
		})
		if err != nil {
			return Solution{}, err
		}
		var res struct {
			ErrorID          int    `json:"errorId"`
			ErrorCode        string `json:"errorCode"`
			ErrorDescription string `json:"errorDescription"`
			Status           string `json:"status"`
			Solution         struct {
				Token              string `json:"token"`
				Text               string `json:"text"`
				GRecaptchaResponse string `json:"gRecaptchaResponse"`
				UserAgent          string `json:"userAgent"`
				Cookies            string `json:"cookies"`
			} `json:"solution"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return Solution{}, fmt.Errorf("captcha: getTaskResult decode: %w", err)
		}
		if res.ErrorID != 0 {
			return Solution{}, fmt.Errorf("captcha: getTaskResult failed: %s %s", res.ErrorCode, res.ErrorDescription)
		}
		if strings.EqualFold(res.Status, "ready") {
			tok := firstNonEmpty(res.Solution.Token, res.Solution.GRecaptchaResponse, res.Solution.Text)
			if tok == "" {
				return Solution{}, fmt.Errorf("captcha: remote solution empty")
			}
			return Solution{Token: tok, Text: tok, Confidence: 0.92}, nil
		}
	}
}

func (r *RemoteSolver) taskPayload(ch Challenge, image []byte) (map[string]any, error) {
	switch ch.Kind {
	case KindImageText, KindMath:
		if len(image) == 0 {
			return nil, fmt.Errorf("captcha: remote image task needs an image")
		}
		if r.Provider == RemoteCapSolver {
			return map[string]any{
				"type":             "ImageToTextTask",
				"module":           "common",
				"body":             base64.StdEncoding.EncodeToString(image),
				"CapMonsterModule": "TextRecognition",
			}, nil
		}
		return map[string]any{
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
