package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/securityaudit"
	"github.com/spf13/cobra"
)

// securityChildAgent runs one agent turn as a child `run --role security`
// process, the same way `review` does, so the audit inherits the user's
// configuration and permission mode and never widens it.
type securityChildAgent struct {
	root, dataDir, model string
	stdin                io.Reader
	stdout, stderr       io.Writer
}

func (a securityChildAgent) Run(ctx context.Context, prompt string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"run", "--cwd", a.root, "--role", "security"}
	if a.dataDir != "" {
		args = append(args, "--data-dir", a.dataDir)
	}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}
	args = append(args, "--", prompt)
	child := exec.CommandContext(ctx, executable, args...)
	child.Stdin = a.stdin
	child.Stdout = a.stdout
	child.Stderr = a.stderr
	return child.Run()
}

func newSecurityCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "security",
		Short: "Defensive security audit whose findings are confirmed by failing proof tests",
	}
	command.AddCommand(newSecurityAuditCommand())
	return command
}

func newSecurityAuditCommand() *cobra.Command {
	var (
		fix, noAgent, skipDeps, asJSON bool
		failOn, model                  string
		proofTimeout                   time.Duration
	)
	command := &cobra.Command{
		Use:   "audit [path]",
		Short: "Audit a project you own and confirm each finding with a failing proof test",
		Long: "Builds a threat model, asks the security agent for vulnerabilities, and requires every " +
			"finding to carry a Go proof test in your project that fails for the stated reason. " +
			"Findings without such a proof are reported as unconfirmed and never as confirmed. " +
			"With --fix the agent repairs confirmed findings and the audit re-runs each proof and the " +
			"full test suite to verify the proof turns green and nothing else regresses. " +
			"Only audit code you own or are authorized to test. Proof tests run in your project.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			var threshold securityaudit.Severity
			if failOn != "" {
				if threshold, err = securityaudit.ParseSeverity(failOn); err != nil {
					return err
				}
			}
			if fix && noAgent {
				return fmt.Errorf("--fix needs the agent; drop --no-agent")
			}
			opts := securityaudit.Options{Root: root, Fix: fix, ProofTimeout: proofTimeout}
			if len(args) == 1 {
				opts.Scope = args[0]
			}
			if !skipDeps {
				opts.Deps = securityaudit.DefaultDependencyScanner
			}
			if !noAgent {
				dataDir, _ := cmd.Flags().GetString("data-dir")
				stdout := cmd.ErrOrStderr()
				opts.Agent = securityChildAgent{
					root: root, dataDir: dataDir, model: model,
					stdin: cmd.InOrStdin(), stdout: stdout, stderr: cmd.ErrOrStderr(),
				}
			}

			report, err := securityaudit.Audit(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if asJSON {
				data, jsonErr := securityaudit.RenderJSON(report)
				if jsonErr != nil {
					return jsonErr
				}
				if _, jsonErr = fmt.Fprintln(cmd.OutOrStdout(), string(data)); jsonErr != nil {
					return jsonErr
				}
			} else if _, err := fmt.Fprint(cmd.OutOrStdout(), securityaudit.RenderMarkdown(report)); err != nil {
				return err
			}
			if threshold != "" {
				if open := report.Blocking(threshold); len(open) > 0 {
					return fmt.Errorf("%d confirmed security finding(s) at or above %s remain open", len(open), threshold)
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&fix, "fix", false, "Have the agent fix confirmed findings, then re-verify the proofs and the full test suite")
	command.Flags().BoolVar(&noAgent, "no-agent", false, "Do not call a model: re-verify the existing findings manifest and report static leads")
	command.Flags().BoolVar(&skipDeps, "skip-deps", false, "Skip the dependency vulnerability check (govulncheck)")
	command.Flags().BoolVar(&asJSON, "json", false, "Print the report as JSON")
	command.Flags().StringVar(&failOn, "fail-on", "", "Exit non-zero when a confirmed finding at or above this severity (critical|high|medium|low|info) is still open")
	command.Flags().StringVar(&model, "model", "", "Model for the agent passes")
	command.Flags().DurationVar(&proofTimeout, "proof-timeout", 0, "Time limit for each proof test run (default 1m)")
	return command
}

func init() { rootCmd.AddCommand(newSecurityCommand()) }
