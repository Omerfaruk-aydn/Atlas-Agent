package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clibridge"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func readProfileFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 256*1024 {
		return nil, fmt.Errorf("%s must be a regular JSON file of at most 256 KiB", path)
	}
	return os.ReadFile(path)
}

func absFromRoot(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func printJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func bridgeStore(cmd *cobra.Command) (string, clibridge.Store, error) {
	root, err := workflowRoot(cmd)
	if err != nil {
		return "", clibridge.Store{}, err
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return "", clibridge.Store{}, err
	}
	return root, clibridge.NewStore(root), nil
}

func findBridgeProfile(store clibridge.Store, value string) (clibridge.Installed, error) {
	ref, err := clibridge.ParseRef(value)
	if err != nil {
		return clibridge.Installed{}, err
	}
	return store.Find(ref)
}

func newCLIBridgeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "cli-bridge",
		Short: "Turn installed CLI programs into validated, bounded Atlas operations",
		Long: "CLI Bridge profiles describe one executable, its allowed operations and typed parameters. " +
			"Parameters become a literal argv shown before it runs; runs go through the guarded bash path with hooks, " +
			"command policy and engineering records. list, inspect, validate, import, preview, pin and remove never run a program.",
	}

	list := &cobra.Command{Use: "list", Short: "List installed profiles in project and user scope", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, store, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		rows, err := store.List()
		if err != nil {
			return err
		}
		type row struct {
			clibridge.Summary
			Executable clibridge.Status `json:"executable_status,omitempty"`
		}
		out := []row{}
		for _, summary := range rows {
			r := row{Summary: summary}
			if summary.Error == "" {
				if installed, err := store.Load(summary.Scope, summary.ID); err == nil {
					_, err := clibridge.Current(cmd.Context(), installed)
					r.Executable = clibridge.StatusOf(err)
				}
			}
			out = append(out, r)
		}
		return printJSON(cmd.OutOrStdout(), map[string]any{"profiles": out, "project_dir": store.ProjectDir, "user_dir": store.UserDir})
	}}

	inspect := &cobra.Command{Use: "inspect <profile>", Short: "Show a profile contract and its pinned executable status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, store, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		installed, err := findBridgeProfile(store, args[0])
		if err != nil {
			return err
		}
		identity, exeErr := clibridge.Current(cmd.Context(), installed)
		executable := map[string]any{"status": clibridge.StatusOf(exeErr), "identity": identity, "pin": installed.Pin}
		if exeErr != nil {
			executable["error"] = exeErr.Error()
		}
		var env []clibridge.EnvStatus
		for _, name := range installed.Profile.Env {
			_, present := os.LookupEnv(name)
			env = append(env, clibridge.EnvStatus{Name: name, Present: present})
		}
		return printJSON(cmd.OutOrStdout(), map[string]any{"scope": installed.Scope, "profile_hash": installed.ProfileSHA256, "imported_at": installed.ImportedAt, "source": installed.Source, "executable": executable, "env": env, "profile": installed.Profile})
	}}

	validate := &cobra.Command{Use: "validate <profile.json>", Short: "Validate a profile file, and optionally render one operation, without running anything", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, _, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		data, err := readProfileFile(absFromRoot(root, args[0]))
		if err != nil {
			return err
		}
		profile, err := clibridge.ParseProfile(data)
		if err != nil {
			return err
		}
		result := map[string]any{"valid": true, "executed": false, "profile_id": profile.ID, "profile_hash": profile.Hash()}
		identity, exeErr := clibridge.Pin(cmd.Context(), profile.Executable)
		result["executable_status"] = clibridge.StatusOf(exeErr)
		result["executable"] = identity
		if exeErr != nil {
			result["executable_error"] = exeErr.Error()
		}
		if op, _ := cmd.Flags().GetString("operation"); op != "" {
			values, _ := cmd.Flags().GetStringArray("arg")
			if exeErr != nil {
				identity = clibridge.Identity{Requested: identity.Requested, Path: cmpString(identity.Path, "<unavailable:"+identity.Requested+">")}
			}
			prep, err := clibridge.Prepare(clibridge.Installed{Profile: profile, ProfileSHA256: profile.Hash()}, identity, op, values, clibridge.RenderOptions{Root: root})
			if err != nil {
				return err
			}
			result["argv"], result["effects"] = prep.DisplayArgv, prep.Effects
		}
		return printJSON(cmd.OutOrStdout(), result)
	}}
	validate.Flags().String("operation", "", "Also render this operation's argv")
	validate.Flags().StringArray("arg", nil, "Parameter value as name=value; repeat for several")

	importCmd := &cobra.Command{Use: "import <profile.json>", Short: "Validate, pin and store a profile; runs no command", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, store, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		path := absFromRoot(root, args[0])
		data, err := readProfileFile(path)
		if err != nil {
			return err
		}
		scopeName, _ := cmd.Flags().GetString("scope")
		scope, err := clibridge.ParseScope(scopeName)
		if err != nil {
			return err
		}
		replace, _ := cmd.Flags().GetBool("replace")
		result, err := store.Import(cmd.Context(), data, clibridge.ImportOptions{Scope: scope, Replace: replace, Source: path})
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), result)
	}}
	importCmd.Flags().String("scope", "project", "project (this repository's .atlas) or user")
	importCmd.Flags().Bool("replace", false, "Overwrite an existing profile with different content")

	pin := &cobra.Command{Use: "pin <profile>", Short: "Trust the current executable again after reviewing an install or upgrade", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, store, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		ref, err := clibridge.ParseRef(args[0])
		if err != nil {
			return err
		}
		installed, err := store.Repin(cmd.Context(), ref)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), map[string]any{"profile": installed.Profile.ID, "scope": installed.Scope, "pin": installed.Pin, "executed": false})
	}}

	remove := &cobra.Command{Use: "remove <profile>", Short: "Delete an installed profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, store, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		ref, err := clibridge.ParseRef(args[0])
		if err != nil {
			return err
		}
		installed, err := store.Remove(cmd.Context(), ref)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), map[string]any{"removed": installed.Profile.ID, "scope": installed.Scope})
	}}

	preview := &cobra.Command{Use: "preview <profile> <operation> [name=value...]", Short: "Show the literal argv and effects without running anything", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		root, store, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		installed, err := findBridgeProfile(store, args[0])
		if err != nil {
			return err
		}
		identity, err := clibridge.Current(cmd.Context(), installed)
		if err != nil {
			return err
		}
		prep, err := clibridge.Prepare(installed, identity, args[1], args[2:], clibridge.RenderOptions{Root: root})
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), map[string]any{"preview": prep, "executed": false})
	}}

	run := &cobra.Command{Use: "run <profile> <operation> [name=value...]", Short: "Run one operation through the guarded bash path, hooks and engineering records", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		allow, _ := cmd.Flags().GetBool("allow-execution")
		if !allow {
			return fmt.Errorf("run executes a program; review `cli-bridge preview` and pass --allow-execution")
		}
		session, _ := cmd.Flags().GetString("session-id")
		if session == "" {
			session = "cli-bridge-" + uuid.NewString()
		}
		prepareID, _ := cmd.Flags().GetString("prepare-id")
		input, _ := json.Marshal(tools.CLIBridgeParams{Action: "run", Profile: args[0], Operation: args[1], Args: args[2:], PrepareID: prepareID})
		response, err := runBridgeTool(cmd, session, string(input))
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "session:", session)
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), response.Content); err != nil {
			return err
		}
		if response.IsError || response.StopTurn {
			return fmt.Errorf("run did not pass")
		}
		return nil
	}}
	run.Flags().Bool("allow-execution", false, "Approve running the previewed program; hooks, command policy and limits still apply")
	run.Flags().String("session-id", "", "Engineering session for the run report (generated when empty)")
	run.Flags().String("prepare-id", "", "Refuse to run unless the argv still matches this preview identity")

	result := &cobra.Command{Use: "result <run-id>", Short: "Show a stored run report", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		session, _ := cmd.Flags().GetString("session-id")
		if session == "" {
			return fmt.Errorf("--session-id is required")
		}
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		_, data, err := store.ReadRecord(cmd.Context(), clibridge.RunNamespace(session), args[0])
		if err != nil {
			return fmt.Errorf("run %s not found in session %s", args[0], session)
		}
		var report clibridge.RunReport
		if err := json.Unmarshal(data, &report); err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), report)
	}}
	result.Flags().String("session-id", "", "Engineering session the run was recorded in")

	draft := &cobra.Command{Use: "draft <executable>", Short: "Create an inactive profile draft from help text for review", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, _, err := bridgeStore(cmd)
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		replace, _ := cmd.Flags().GetBool("replace")
		declared := clibridge.Executable{Name: args[0]}
		if filepath.IsAbs(args[0]) {
			declared = clibridge.Executable{Path: args[0]}
		}
		var help string
		capture := clibridge.HelpCapture{Source: "file"}
		var identity *clibridge.Identity
		if file, _ := cmd.Flags().GetString("help-file"); file != "" {
			data, err := os.ReadFile(absFromRoot(root, file))
			if err != nil {
				return err
			}
			help = string(data)
			if resolved, err := clibridge.Pin(cmd.Context(), declared); err == nil {
				identity = &resolved
			}
		} else {
			allow, _ := cmd.Flags().GetBool("allow-execution")
			if !allow {
				return fmt.Errorf("capturing help runs the program; pass --allow-execution, or supply saved text with --help-file")
			}
			resolved, err := clibridge.Pin(cmd.Context(), declared)
			if err != nil {
				return err
			}
			identity = &resolved
			helpArgs, _ := cmd.Flags().GetStringArray("help-arg")
			if len(helpArgs) == 0 {
				helpArgs = []string{"--help"}
			}
			argv := append([]string{resolved.Path}, helpArgs...)
			text, exit, err := runBridgeHelp(cmd, root, argv)
			if err != nil {
				return err
			}
			help, capture = text, clibridge.HelpCapture{Source: "program", Argv: argv, ExitCode: &exit}
		}
		draftDoc, err := clibridge.NewDraft(id, declared, identity, help, capture)
		if err != nil {
			return err
		}
		path, err := clibridge.WriteDraft(root, draftDoc, replace)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), map[string]any{"draft_path": path, "candidates": len(draftDoc.Candidates), "notes": draftDoc.Notes, "active": false})
	}}
	draft.Flags().String("id", "", "Profile id for the draft")
	_ = draft.MarkFlagRequired("id")
	draft.Flags().String("help-file", "", "Read saved help text instead of running the program")
	draft.Flags().StringArray("help-arg", nil, "Literal help argument (default --help); repeat for several")
	draft.Flags().Bool("allow-execution", false, "Approve running the program's help through the guarded path")
	draft.Flags().Bool("replace", false, "Overwrite an existing draft")

	examples := &cobra.Command{Use: "examples [name]", Short: "List bundled example profiles or print one", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return printJSON(cmd.OutOrStdout(), map[string]any{"examples": clibridge.ExampleNames(), "note": "Examples are inert until imported: `atlas-agent cli-bridge examples ffmpeg-video > video.json`, review, then import."})
		}
		data, ok := clibridge.Examples()[args[0]]
		if !ok {
			return fmt.Errorf("unknown example %q; available: %s", args[0], strings.Join(clibridge.ExampleNames(), ", "))
		}
		_, err := cmd.OutOrStdout().Write(data)
		return err
	}}

	command.AddCommand(list, inspect, validate, importCmd, pin, remove, preview, run, result, draft, examples)
	return command
}

func cmpString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// bridgeRuntime wires the model-free path: the bash tool and the cli_bridge
// tool, each behind configured hooks and the execution policy, with every
// nested call recorded in the engineering store like an agent tool call.
type bridgeRuntime struct {
	root    string
	session string
	cfg     *config.ConfigStore
	store   *engineering.Store
	bash    fantasy.AgentTool
	pre     *hooks.Runner
	post    *hooks.Runner
}

func newBridgeRuntime(cmd *cobra.Command, session string) (*bridgeRuntime, error) {
	root, _, err := bridgeStore(cmd)
	if err != nil {
		return nil, err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	cfg, err := config.Load(root, dataDir, false)
	if err != nil {
		return nil, err
	}
	if _, ok := cfg.Config().Agents[config.AgentCoder]; !ok {
		// No model is needed here, but disabled_tools and usage profiles
		// must still decide which tools exist, as they would for an agent.
		cfg.SetupAgents()
	}
	allowed := cfg.Config().Agents[config.AgentCoder].AllowedTools
	if !slices.Contains(allowed, tools.BashToolName) || !slices.Contains(allowed, tools.CLIBridgeToolName) {
		return nil, fmt.Errorf("bash and cli_bridge must both be enabled in this workspace's configuration")
	}
	store := engineering.NewStore(cfg.Config().Options.DataDirectory)
	pre := hooks.NewRunner(cfg.Config().Hooks[hooks.EventPreToolUse], root, root)
	post := hooks.NewRunner(cfg.Config().Hooks[hooks.EventPostToolUse], root, root)
	policy := tools.ExecutionPolicy(cfg.Config())
	if policy.Mode != "" && policy.Mode != "legacy" {
		binding := execution.Binding{Root: root, Store: store, Factory: func(ctx context.Context) (execution.Runner, error) {
			return execution.NewRunner(ctx, policy, store)
		}}
		pre, post = pre.WithExecution(binding), post.WithExecution(binding)
	}
	// The explicit --allow-execution flag is the approval; hooks, command
	// policy and execution policy remain in force.
	permissions := permission.NewPermissionService(root, true, nil)
	bash := tools.NewBashTool(permissions, root, cfg.Config().Options.Attribution, "", tools.NewCommandPolicy(cfg.Config()), tools.NewBashLimits(cfg.Config()))
	bash = agent.WithToolHooks(tools.WithSecretFilter(bash), pre, post)
	return &bridgeRuntime{root: root, session: session, cfg: cfg, store: store, bash: tools.WithExecution(bash, cfg, store), pre: pre, post: post}, nil
}

// invoke dispatches nested bash calls with operation accounting.
func (r *bridgeRuntime) invoke(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if call.Name != tools.BashToolName {
		return fantasy.NewTextErrorResponse("unsupported nested tool for the command-line runner: " + call.Name), nil
	}
	operation, err := r.store.Begin(ctx, r.session, call.ID, call.Name, call.Input, "")
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	response, runErr := r.bash.Run(ctx, call)
	if ctx.Err() != nil {
		return response, runErr
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	passed := tools.ToolSucceeded(call.Name, response, runErr)
	if err := r.store.FinishObserved(cleanup, r.session, operation, passed, false, tools.ToolEvidenceHash(call.Name, response, runErr), tools.ToolOutcomeObserved(call.Name, response, runErr)); err != nil {
		return response, err
	}
	return response, runErr
}

func runBridgeTool(cmd *cobra.Command, session, input string) (fantasy.ToolResponse, error) {
	rt, err := newBridgeRuntime(cmd, session)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	tool := tools.NewCLIBridgeTool(tools.CLIBridgeServices{Root: rt.root, Store: rt.store, Permissions: permission.NewPermissionService(rt.root, true, nil), Invoke: rt.invoke})
	tool = tools.WithExecution(agent.WithToolHooks(tools.WithSecretFilter(tool), rt.pre, rt.post), rt.cfg, rt.store)
	ctx := context.WithValue(cmd.Context(), tools.SessionIDContextKey, session)
	return tool.Run(ctx, fantasy.ToolCall{ID: uuid.NewString(), Name: tools.CLIBridgeToolName, Input: input})
}

func runBridgeHelp(cmd *cobra.Command, root string, argv []string) (string, int, error) {
	session := "cli-bridge-draft-" + uuid.NewString()
	rt, err := newBridgeRuntime(cmd, session)
	if err != nil {
		return "", 0, err
	}
	params, _ := json.Marshal(tools.BashParams{Description: "cli-bridge help capture", Argv: argv, WorkingDir: root})
	ctx, cancel := context.WithTimeout(context.WithValue(cmd.Context(), tools.SessionIDContextKey, session), 30*time.Second)
	defer cancel()
	ctx = tools.WithArgvOptions(ctx, tools.ArgvOptions{StreamLimit: clibridge.MaxHelpBytes})
	response, err := rt.invoke(ctx, fantasy.ToolCall{ID: uuid.NewString(), Name: tools.BashToolName, Input: string(params)})
	if err != nil {
		return "", 0, fmt.Errorf("help capture did not complete: %w", err)
	}
	if response.IsError || response.StopTurn {
		return "", 0, fmt.Errorf("help capture refused: %s", response.Content)
	}
	var meta tools.BashResponseMetadata
	if json.Unmarshal([]byte(response.Metadata), &meta) != nil || meta.ExitCode == nil {
		return "", 0, fmt.Errorf("help capture produced no observable result")
	}
	text := meta.Output
	if meta.Streams != nil {
		text = strings.TrimSpace(meta.Streams.Stdout + "\n" + meta.Streams.Stderr)
	}
	return text, *meta.ExitCode, nil
}

func init() {
	rootCmd.AddCommand(newCLIBridgeCommand())
}
