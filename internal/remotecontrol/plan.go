package remotecontrol

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

// Step identifiers, in execution order.
const (
	StepLock     = "lock"
	StepPackages = "packages"
	StepFirewall = "firewall"
	StepBackup   = "backup"
	StepBinary   = "binary"
	StepKey      = "key"
	StepConfig   = "config"
	StepStart    = "start"
	StepHealth   = "health"
	StepState    = "state"
	StepUnlock   = "unlock"
	StepHTTPS    = "https"
	StepLink     = "relay_link"
	StepRollback = "rollback"
	StepRemove   = "remove"
)

// Plan is what an installation will do, shown before approval. Hash binds
// an approval to exactly this scope.
type Plan struct {
	ProfileID  string `json:"profile_id"`
	Target     string `json:"target"`
	PublicHost string `json:"public_host"`
	Update     bool   `json:"update"`

	Artifact struct {
		Arch    string `json:"arch"`
		SHA256  string `json:"sha256"`
		Version string `json:"version"`
	} `json:"artifact"`

	Packages      []string `json:"packages,omitempty"`
	FirewallRules []string `json:"firewall_rules,omitempty"`
	ClearLock     bool     `json:"clear_lock,omitempty"`
	ReplaceKey    bool     `json:"replace_key,omitempty"`
	InstallKey    bool     `json:"install_key,omitempty"`
	HostChanged   bool     `json:"host_changed,omitempty"`
	Email         string   `json:"email,omitempty"`

	Warnings []Message `json:"-"`
	Hash     string    `json:"-"`
}

// BuildPlan derives the plan from an assessment that is Ready.
func BuildPlan(p config.RemoteProfile, rep Report, a Assessment) Plan {
	plan := Plan{
		ProfileID: p.ID, Target: p.SSHUser + "@" + HostAddress(p.SSHHost, p.Port()),
		PublicHost: p.PublicHost, Update: rep.Installed && rep.Managed && rep.State != nil,
		Packages: a.InstallPackages, FirewallRules: a.FirewallRules,
		ClearLock: a.ClearStaleLock, ReplaceKey: a.ReplaceKey, InstallKey: !rep.KeyExists,
		HostChanged: a.HostChanged, Email: p.ACMEEmail, Warnings: a.Warnings,
	}
	plan.Artifact.Arch, plan.Artifact.SHA256, plan.Artifact.Version = a.Artifact.Arch, a.Artifact.SHA256, a.Artifact.Version
	data, _ := json.Marshal(plan)
	plan.Hash = sha256Hex(data)
	return plan
}

// Within reports whether p needs nothing beyond the approved plan, so an
// interrupted installation can continue under its approval. Progress made
// by the earlier attempt only removes work (packages already installed, the
// key already uploaded); anything new needs a new approval.
func (p Plan) Within(approved Plan) bool {
	subset := func(a, b []string) bool {
		for _, x := range a {
			if !slices.Contains(b, x) {
				return false
			}
		}
		return true
	}
	return p.ProfileID == approved.ProfileID && p.Target == approved.Target && p.PublicHost == approved.PublicHost &&
		p.Artifact == approved.Artifact && p.Email == approved.Email && p.Update == approved.Update &&
		subset(p.Packages, approved.Packages) && subset(p.FirewallRules, approved.FirewallRules) &&
		(!p.ReplaceKey || approved.ReplaceKey) && (!p.HostChanged || approved.HostChanged)
}

// Steps lists the remote steps this plan runs, in order.
func (p Plan) Steps() []string {
	steps := []string{StepLock}
	if len(p.Packages) > 0 {
		steps = append(steps, StepPackages)
	}
	if len(p.FirewallRules) > 0 {
		steps = append(steps, StepFirewall)
	}
	if p.Update {
		steps = append(steps, StepBackup)
	}
	steps = append(steps, StepBinary)
	if p.InstallKey || p.ReplaceKey {
		steps = append(steps, StepKey)
	}
	return append(steps, StepConfig, StepStart, StepHealth, StepState, StepUnlock, StepHTTPS, StepLink)
}

// StepTitle names a step for progress lists.
func StepTitle(step string) Message {
	switch step {
	case StepLock:
		return Msg("Reserve the server for this installation")
	case StepPackages:
		return Msg("Install Docker packages from the distribution")
	case StepFirewall:
		return Msg("Open HTTP and HTTPS in ufw")
	case StepBackup:
		return Msg("Back up the current installation")
	case StepBinary:
		return Msg("Upload and verify the relay binary")
	case StepKey:
		return Msg("Install the connector key")
	case StepConfig:
		return Msg("Write the service configuration")
	case StepStart:
		return Msg("Start or update the services")
	case StepHealth:
		return Msg("Check that both services are running")
	case StepState:
		return Msg("Record the installed version")
	case StepUnlock:
		return Msg("Release the server")
	case StepHTTPS:
		return Msg("Verify HTTPS from this computer")
	case StepLink:
		return Msg("Verify the relay link to this computer")
	case StepRollback:
		return Msg("Restore the previous version")
	case StepRemove:
		return Msg("Remove the relay service")
	}
	return Msg("Unknown step")
}

// Describe renders the plan for the approval dialog. text localizes.
func (p Plan) Describe(text func(string) string) string {
	l := func(m Message) string { return m.Localize(text) }
	var b strings.Builder
	line := func(m Message) { b.WriteString(l(m) + "\n") }
	item := func(m Message) { b.WriteString("  • " + l(m) + "\n") }
	if p.Update {
		line(Msg("Update the Atlas relay on %s for https://%s.", p.Target, p.PublicHost))
	} else {
		line(Msg("Install the Atlas relay on %s for https://%s.", p.Target, p.PublicHost))
	}
	b.WriteString("\n")
	line(Msg("Components:"))
	item(Msg("Atlas mobile relay %s for linux/%s, SHA-256 %s…, bundled in this Atlas build", p.Artifact.Version, p.Artifact.Arch, short(p.Artifact.SHA256)))
	item(Msg("Caddy 2 (caddy:2-alpine) for HTTPS with an automatic Let's Encrypt or ZeroSSL certificate"))
	item(Msg("Distroless runtime image gcr.io/distroless/static-debian12:nonroot"))
	if len(p.Packages) > 0 {
		item(Msg("Distribution packages: %s", strings.Join(p.Packages, ", ")))
	}
	b.WriteString("\n")
	line(Msg("Files and services:"))
	item(Msg("%s: compose.yaml, Caddyfile, .env, bin/atlas-mobile-relay, state.json", RemoteDir))
	if p.InstallKey || p.ReplaceKey {
		item(Msg("%s, readable only by the relay account", RemoteKeyFile))
	}
	item(Msg("Docker Compose project %s with the services relay and caddy", ComposeName))
	if p.Update {
		item(Msg("A backup of the current files in %s/backups (the three newest are kept)", RemoteDir))
	}
	b.WriteString("\n")
	line(Msg("Privileges: root or sudo for the steps above."))
	line(Msg("Ports: TCP 80 and 443 and UDP 443 are published by Caddy; the relay itself is not published."))
	if len(p.FirewallRules) > 0 {
		line(Msg("Firewall: ufw rules are added for %s. Existing rules are not changed.", strings.Join(p.FirewallRules, ", ")))
	} else {
		line(Msg("Firewall: no changes."))
	}
	line(Msg("DNS: not changed by Atlas."))
	if p.ReplaceKey {
		line(Msg("The connector key on the server is replaced; another computer using it is disconnected."))
	}
	if p.HostChanged {
		line(Msg("The relay switches to a new hostname; phones must pair again."))
	}
	if p.ClearLock {
		line(Msg("An abandoned installation lock is cleared."))
	}
	b.WriteString("\n")
	if p.Update {
		line(Msg("Rollback: if the updated services do not start, the backup is restored automatically. You can also restore it later from /rc."))
	} else {
		line(Msg("Rollback: Remove service in /rc deletes everything listed above except installed packages and firewall rules."))
	}
	if len(p.Warnings) > 0 {
		b.WriteString("\n")
		line(Msg("Warnings:"))
		for _, w := range p.Warnings {
			item(w)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
