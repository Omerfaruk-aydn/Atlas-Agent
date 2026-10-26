package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/plugin"
	"github.com/spf13/cobra"
)

func pluginRoot(cmd *cobra.Command) (string, error) {
	root, err := workflowRoot(cmd)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".atlas", "plugins"), nil
}

func newPluginCommand() *cobra.Command {
	command := &cobra.Command{Use: "plugin", Short: "Install and inspect portable Atlas customization packages"}
	validate := &cobra.Command{Use: "validate <directory>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		manifest, err := plugin.Load(args[0])
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(manifest)
	}}
	initCommand := &cobra.Command{Use: "init <name>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := pluginRoot(cmd)
		if err != nil {
			return err
		}
		directory := filepath.Join(root, args[0])
		manifest := plugin.Manifest{Name: args[0], Version: "0.1.0", Contents: []string{"skills"}, Description: "Atlas plugin package"}
		data, _ := json.MarshalIndent(manifest, "", "  ")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
		path := filepath.Join(directory, plugin.ManifestFile)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("plugin manifest already exists")
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), path)
		return nil
	}}
	install := &cobra.Command{Use: "install <directory-or-git-url>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]
		if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "ssh://") || strings.HasPrefix(source, "git@") {
			temporary, err := os.MkdirTemp("", "atlas-plugin-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(temporary)
			if output, err := exec.CommandContext(cmd.Context(), "git", "clone", "--depth=1", "--", source, temporary).CombinedOutput(); err != nil {
				return fmt.Errorf("clone plugin: %w: %s", err, output)
			}
			source = temporary
		}
		manifest, err := plugin.Load(source)
		if err != nil {
			return err
		}
		confirmed, _ := cmd.Flags().GetBool("yes")
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"preview": manifest, "warning": "Review all package contents; hooks and MCP can execute code."}); err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("installation stopped after preview; rerun with --yes after reviewing the package")
		}
		root, err := pluginRoot(cmd)
		if err != nil {
			return err
		}
		replace, _ := cmd.Flags().GetBool("replace")
		installed, err := plugin.Install(source, root, replace)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"installed": installed.Name, "version": installed.Version})
	}}
	install.Flags().Bool("yes", false, "Approve the displayed content and permission preview")
	install.Flags().Bool("replace", false, "Replace an existing package after explicit review")
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		root, err := pluginRoot(cmd)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			return json.NewEncoder(cmd.OutOrStdout()).Encode([]plugin.Manifest{})
		}
		if err != nil {
			return err
		}
		manifests := []plugin.Manifest{}
		for _, entry := range entries {
			if entry.IsDir() {
				manifest, err := plugin.Load(filepath.Join(root, entry.Name()))
				if err != nil {
					return err
				}
				manifests = append(manifests, manifest)
			}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(manifests)
	}}
	remove := &cobra.Command{Use: "remove <name>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := pluginRoot(cmd)
		if err != nil {
			return err
		}
		manifest, err := plugin.Load(filepath.Join(root, args[0]))
		if err != nil {
			return err
		}
		if manifest.Name != args[0] {
			return fmt.Errorf("plugin directory and manifest name differ")
		}
		return os.RemoveAll(filepath.Join(root, args[0]))
	}}
	command.AddCommand(initCommand, install, list, remove, validate)
	return command
}

func init() { rootCmd.AddCommand(newPluginCommand()) }
