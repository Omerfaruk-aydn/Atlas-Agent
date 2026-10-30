package tools

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/securityaudit"
)

const SecurityVerifyToolName = "security_verify"

//go:embed security_verify.md
var securityVerifyDescription string

type SecurityVerifyParams struct {
	FindingID string `json:"finding_id,omitempty" description:"Verify only this finding. Defaults to every finding in the manifest."`
	Manifest  string `json:"manifest,omitempty" description:"Path to the findings manifest. Defaults to .atlas/security/findings.json."`
}

type SecurityVerifyResponseMetadata struct {
	Confirmed   int `json:"confirmed"`
	Unconfirmed int `json:"unconfirmed"`
}

func NewSecurityVerifyTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SecurityVerifyToolName,
		securityVerifyDescription,
		func(ctx context.Context, params SecurityVerifyParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			path := cmp.Or(params.Manifest, securityaudit.ManifestPath(securityaudit.DefaultStateDir(workingDir)))
			if !filepath.IsAbs(path) {
				path = filepath.Join(workingDir, path)
			}
			findings, err := securityaudit.LoadManifest(path)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if len(findings) == 0 {
				return fantasy.NewTextErrorResponse("no findings in " + path + "; write the manifest first"), nil
			}

			verifier := securityaudit.Verifier{Root: workingDir}
			var (
				b    strings.Builder
				meta SecurityVerifyResponseMetadata
			)
			matched := false
			for i := range findings {
				f := &findings[i]
				if params.FindingID != "" && f.ID != params.FindingID {
					continue
				}
				matched = true
				verifier.Verify(ctx, f)
				if f.Status == securityaudit.StatusConfirmed {
					meta.Confirmed++
				} else {
					meta.Unconfirmed++
				}
				fmt.Fprintf(&b, "[%s] %s (%s, %s): %s\n", f.ID, f.Status, f.Severity, f.CWE, f.Reason)
				if f.Evidence != "" {
					fmt.Fprintf(&b, "  failure output: %s\n", strings.ReplaceAll(f.Evidence, "\n", "\n  "))
				}
			}
			if !matched {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("no finding with id %q in the manifest", params.FindingID)), nil
			}
			b.WriteString("\nOnly \"confirmed\" findings are demonstrated. Fix the proof test for anything else, or leave it unconfirmed.\n")
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
		},
	)
}
