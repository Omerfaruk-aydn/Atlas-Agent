package policy

import (
	"path"
	"regexp"
	"strings"
)

var (
	downloaders = map[string]bool{"curl": true, "wget": true, "iwr": true, "irm": true, "invoke-webrequest": true, "invoke-restmethod": true}
	// interpreters read a program from stdin or run a named file.
	interpreters = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "python": true, "python3": true, "node": true, "perl": true, "ruby": true, "iex": true, "invoke-expression": true, "php": true}

	sensitivePathRe = regexp.MustCompile(`(?i)(^|/)(\.env(\.[^/]*)?|id_rsa|id_ed25519|id_ecdsa|\.netrc|\.npmrc|\.pypirc|credentials|\.git-credentials|\.aws|\.ssh|\.gnupg|\.kube/config)(/|$)|\.(pem|key|p12|pfx|kdbx)$`)
	shellRcRe       = regexp.MustCompile(`(?i)(^|/)(\.bashrc|\.bash_profile|\.zshrc|\.zprofile|\.profile|authorized_keys|\.ssh/config)$`)
	dangerousRmRe   = regexp.MustCompile(`^(/|/\*|~|~/|~/\*|\$HOME|\$\?|\.\.|[A-Za-z]:[\\/]?|[A-Za-z]:[\\/]\*)$`)
)

func hasFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

// shortFlag reports whether any short-option cluster contains letter, so
// "-rf" and "-fr" both count for r and f.
func shortFlag(args []string, letter byte) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.IndexByte(a, letter) > 0 {
			return true
		}
	}
	return false
}

func builtinRules() []Rule {
	return []Rule{
		{
			ID: "download-pipe-exec", Action: Deny, Enforce: true,
			Reason: "A downloaded script would run without being read first.",
			Remedy: "Download to a file, read it, then run it.",
			Match: func(c Call, _ *Context) bool {
				return downloaders[c.PipedFrom] && interpreters[c.Name()]
			},
		},
		{
			ID: "rm-system-root", Action: Deny, Enforce: true,
			Reason: "This removes a root, home or drive directory.",
			Remedy: "Name the specific project path to delete.",
			Match: func(c Call, _ *Context) bool {
				if c.Name() != "rm" && c.Name() != "rmdir" && c.Name() != "remove-item" && c.Name() != "del" {
					return false
				}
				recursive := shortFlag(c.Args(), 'r') || shortFlag(c.Args(), 'R') || hasFlag(c.Args(), "--recursive", "-Recurse")
				if !recursive {
					return false
				}
				for _, a := range c.Args() {
					if dangerousRmRe.MatchString(a) {
						return true
					}
				}
				return false
			},
		},
		{
			ID: "raw-disk-write", Action: Deny, Enforce: true,
			Reason: "This writes directly to a disk device or formats a filesystem.",
			Remedy: "Do this yourself outside the agent if it is intended.",
			Match: func(c Call, _ *Context) bool {
				switch c.Name() {
				case "mkfs", "fdisk", "format-volume", "diskpart":
					return true
				case "dd":
					for _, a := range c.Args() {
						if strings.HasPrefix(a, "of=/dev/") || strings.HasPrefix(a, `of=\\.\`) {
							return true
						}
					}
				}
				return strings.HasPrefix(c.Name(), "mkfs.")
			},
		},
		{
			ID: "secret-upload", Action: Deny, Enforce: true,
			Reason: "A credential or key file would be sent to a remote host.",
			Remedy: "Do not send secrets; share only the specific non-secret data needed.",
			Match: func(c Call, _ *Context) bool {
				if !downloaders[c.Name()] {
					return false
				}
				for _, a := range c.Args() {
					if i := strings.Index(a, "@"); i >= 0 && sensitivePathRe.MatchString(a[i+1:]) {
						return true
					}
				}
				return false
			},
		},
		{
			ID: "data-in-url", Action: Ask, Enforce: true,
			Reason: "A value computed by the shell is placed in a request URL, which can leak data to the host.",
			Remedy: "Confirm what the expansion contains, or send it in a reviewed body instead.",
			Match: func(c Call, _ *Context) bool {
				if !downloaders[c.Name()] {
					return false
				}
				for _, a := range c.Args() {
					if (strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://")) && strings.Contains(a, "$?") {
						return true
					}
				}
				return false
			},
		},
		{
			ID: "upload-to-remote", Action: Ask, Enforce: true,
			Reason: "A local file would be uploaded to a remote host.",
			Remedy: "Confirm the file and the destination.",
			Match: func(c Call, _ *Context) bool {
				if c.Name() != "curl" && c.Name() != "wget" {
					return false
				}
				for _, a := range c.Args() {
					if a == "-T" || a == "--upload-file" || a == "--post-file" {
						return true
					}
					if (strings.HasPrefix(a, "@") && len(a) > 1) || strings.Contains(a, "=@") {
						return true
					}
				}
				return false
			},
		},
		{
			ID: "git-history-destroy", Action: Ask, Enforce: true,
			Reason: "This discards work or history that git cannot bring back.",
			Remedy: "Confirm, or use a reversible form (git stash, a new branch).",
			Match: func(c Call, _ *Context) bool {
				if c.Name() != "git" {
					return false
				}
				sub, rest := gitSubcommand(c.Args())
				switch sub {
				case "reset":
					return hasFlag(rest, "--hard")
				case "clean":
					return shortFlag(rest, 'f') || hasFlag(rest, "--force")
				case "checkout", "restore":
					return hasFlag(rest, "--", ".") && hasFlag(rest, ".")
				case "branch":
					return hasFlag(rest, "-D") || (hasFlag(rest, "-d") && hasFlag(rest, "--force"))
				case "stash":
					return len(rest) > 0 && (rest[0] == "drop" || rest[0] == "clear")
				case "reflog":
					return len(rest) > 0 && rest[0] == "expire"
				}
				return false
			},
		},
		{
			ID: "shell-startup-write", Action: Ask, Enforce: true,
			Reason: "This changes a shell startup or SSH file that affects every future session.",
			Remedy: "Confirm the exact change.",
			Match: func(c Call, _ *Context) bool {
				for _, t := range c.Redirects {
					if shellRcRe.MatchString(strings.ReplaceAll(t, `\`, "/")) {
						return true
					}
				}
				switch c.Name() {
				case "tee", "cp", "mv", "ln", "sed":
					for _, a := range c.Args() {
						if shellRcRe.MatchString(strings.ReplaceAll(a, `\`, "/")) {
							return true
						}
					}
				}
				return false
			},
		},
		{
			ID: "world-writable", Action: Ask, Enforce: true,
			Reason: "This makes files writable or executable by everyone.",
			Remedy: "Use a narrower mode such as 755 or 644.",
			Match: func(c Call, _ *Context) bool {
				if c.Name() != "chmod" {
					return false
				}
				for _, a := range c.Args() {
					if a == "777" || a == "a+w" || a == "o+w" || a == "0777" {
						return shortFlag(c.Args(), 'R') || hasFlag(c.Args(), "--recursive")
					}
				}
				return false
			},
		},
		{
			ID: "privilege-escalation", Action: Ask, Enforce: true,
			Reason: "This runs with elevated privileges.",
			Remedy: "Confirm that elevation is needed.",
			Match: func(c Call, _ *Context) bool {
				for _, w := range c.Wrapper {
					if w == "sudo" || w == "doas" {
						return true
					}
				}
				return false
			},
		},
		{
			// Rolling out: recorded in the audit log but not yet enforced.
			ID: "git-remote-rewrite", Action: Ask, Enforce: false,
			Reason: "This points the repository at a different remote.",
			Remedy: "Confirm the new URL.",
			Match: func(c Call, _ *Context) bool {
				if c.Name() != "git" {
					return false
				}
				sub, rest := gitSubcommand(c.Args())
				return sub == "remote" && len(rest) > 0 && (rest[0] == "set-url" || rest[0] == "add")
			},
		},
	}
}

// gitSubcommand returns the git subcommand and its arguments, skipping
// global options such as -C <dir> and -c k=v.
func gitSubcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a, args[i+1:]
		}
	}
	return "", nil
}

// pushMatrix applies the push rules: a plain push to a feature branch is
// allowed; force pushes, deletions and pushes to protected branches ask.
func pushMatrix(c Call, x *Context) *Finding {
	if c.Name() != "git" {
		return nil
	}
	sub, rest := gitSubcommand(c.Args())
	if sub != "push" {
		return nil
	}
	force := hasFlag(rest, "--force", "-f", "--force-with-lease", "--mirror") || shortFlag(rest, 'f')
	del := hasFlag(rest, "--delete", "-d")
	var positional []string
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
		}
	}
	var targets []string
	if len(positional) >= 2 {
		for _, spec := range positional[1:] {
			if _, dst, ok := strings.Cut(spec, ":"); ok {
				spec = dst
			}
			targets = append(targets, strings.TrimPrefix(spec, "+"))
		}
	} else if x.Branch != "" && !del {
		targets = []string{x.Branch}
	}
	f := func(reason, remedy string) *Finding {
		return &Finding{RuleID: "push-matrix", Action: Ask, Enforced: true, Reason: reason, Remedy: remedy, Command: strings.Join(c.Argv, " ")}
	}
	switch {
	case force:
		return f("A force push can overwrite other people's commits.", "Push without --force, or confirm.")
	case del:
		return f("This deletes a remote branch.", "Confirm the branch name.")
	case len(targets) == 0:
		return f("The branch being pushed is not known.", "Name the remote and branch explicitly.")
	}
	for _, t := range targets {
		if t == "HEAD" && x.Branch != "" {
			t = x.Branch
		}
		if x.isProtected(t) {
			return f("This pushes to a protected branch ("+path.Base(t)+").", "Push a feature branch and open a pull request, or confirm.")
		}
	}
	return nil
}
