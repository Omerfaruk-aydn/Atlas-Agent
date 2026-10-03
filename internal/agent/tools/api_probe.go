package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

//go:embed api_probe.md
var apiProbeDescription string

type APIProbeParams struct {
	URL            string   `json:"url"`
	Method         string   `json:"method,omitempty" description:"GET or HEAD; default GET."`
	ExpectedStatus int      `json:"expected_status,omitempty" description:"Expected HTTP status, default 200."`
	RequiredKeys   []string `json:"required_keys,omitempty" description:"At most 32 required top-level JSON object keys."`
}

func NewAPIProbeTool(root string, perms permission.Service, policy URLPolicy) fantasy.AgentTool {
	return fantasy.NewAgentTool("api_probe", apiProbeDescription, func(ctx context.Context, p APIProbeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if p.Method == "" {
			p.Method = http.MethodGet
		}
		if p.ExpectedStatus == 0 {
			p.ExpectedStatus = http.StatusOK
		}
		parsed, err := url.Parse(p.URL)
		if err != nil || len(p.URL) > 4096 || parsed == nil || parsed.User != nil || parsed.Fragment != "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fantasy.NewTextErrorResponse("provide an HTTP(S) URL without credentials or fragment"), nil
		}
		if (p.Method != http.MethodGet && p.Method != http.MethodHead) || p.ExpectedStatus < 100 || p.ExpectedStatus > 599 || len(p.RequiredKeys) > 32 || (p.Method == http.MethodHead && len(p.RequiredKeys) > 0) {
			return fantasy.NewTextErrorResponse("invalid method, status or JSON assertions"), nil
		}
		for _, key := range p.RequiredKeys {
			if strings.TrimSpace(key) == "" || len(key) > 256 {
				return fantasy.NewTextErrorResponse("invalid JSON key"), nil
			}
		}
		if err := policy.Check(p.URL); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if perms == nil || GetSessionFromContext(ctx) == "" {
			return fantasy.NewTextErrorResponse("API probe requires permissions and a session"), nil
		}
		approved, err := perms.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), Path: root, ToolCallID: call.ID, ToolName: "api_probe", Action: "fetch", Description: fmt.Sprintf("Probe %s %s", p.Method, p.URL), Params: p})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !approved {
			return NewPermissionDeniedResponse(perms), nil
		}
		requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(requestCtx, p.Method, p.URL, nil)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		start := time.Now()
		response, err := client.Do(request)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if len(body) > 256*1024 {
			return fantasy.NewTextErrorResponse("API response exceeds 256KiB"), nil
		}
		missing := make([]string, 0)
		if len(p.RequiredKeys) > 0 {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(body, &object); err != nil || object == nil {
				return fantasy.NewTextErrorResponse("API response is not a JSON object"), nil
			}
			for _, key := range p.RequiredKeys {
				if _, ok := object[key]; !ok {
					missing = append(missing, key)
				}
			}
		}
		passed := response.StatusCode == p.ExpectedStatus && len(missing) == 0
		data, err := json.Marshal(map[string]any{"passed": passed, "status": response.StatusCode, "expected_status": p.ExpectedStatus, "missing_keys": missing, "duration_ms": time.Since(start).Milliseconds(), "bytes": len(body), "content_type": response.Header.Get("Content-Type")})
		result := fantasy.NewTextResponse(string(data))
		result.IsError = !passed
		return fantasy.WithResponseMetadata(result, MeasuredCheckMetadata{Observed: true, Passed: passed}), err
	})
}
