package remotecontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol/relaybin"
)

// RemoteState is /opt/atlas-mobile-relay/state.json, written by /rc after
// a verified installation step. It holds no secrets.
type RemoteState struct {
	ManagedBy   string `json:"managed_by"`
	Version     string `json:"version"`
	SHA256      string `json:"sha256"`
	Arch        string `json:"arch"`
	PublicHost  string `json:"public_host"`
	InstalledAt string `json:"installed_at"`
	KeySHA256   string `json:"key_sha256"`
}

// Report is what a read-only preflight learned about a server.
type Report struct {
	At       time.Time `json:"at"`
	RemoteIP string    `json:"remote_ip"`

	Kernel    string `json:"kernel"`
	Machine   string `json:"machine"`
	Arch      string `json:"arch"`
	OSID      string `json:"os_id"`
	OSLike    string `json:"os_like"`
	OSVersion string `json:"os_version"`
	OSName    string `json:"os_name"`

	Privilege string `json:"privilege"` // root, passwordless, none
	Systemd   bool   `json:"systemd"`
	Apt       bool   `json:"apt"`
	DiskKB    int64  `json:"disk_kb"`
	MemKB     int64  `json:"mem_kb"`
	Addresses []string

	Docker         bool   `json:"docker"`
	DockerVersion  string `json:"docker_version"`
	ComposeVersion string `json:"compose_version"`

	PortsKnown bool     `json:"ports_known"`
	TCPListen  []string `json:"tcp_listen"`
	UDPListen  []string `json:"udp_listen"`

	UFW       string   `json:"ufw"` // active, inactive, none, ""
	UFWRules  []string `json:"ufw_rules"`
	Firewalld string   `json:"firewalld"`

	Installed         bool              `json:"installed"`
	Managed           bool              `json:"managed"`
	State             *RemoteState      `json:"state,omitempty"`
	Lock              string            `json:"lock"`
	BinarySHA         string            `json:"binary_sha"`
	KeyExists         bool              `json:"key_exists"`
	KeySHA            string            `json:"key_sha"`
	Backups           []string          `json:"backups"`
	Services          map[string]string `json:"services"`
	ProjectContainers []string          `json:"project_containers"`

	DNS DNSCheck `json:"dns"`
	// Complete is false when the script output was cut short.
	Complete bool `json:"complete"`
}

// DNSCheck relates the public hostname to the server.
type DNSCheck struct {
	Host      string   `json:"host"`
	Addresses []string `json:"addresses"`
	// Matches is true when a record equals the SSH address or one of the
	// server's own interface addresses.
	Matches bool   `json:"matches"`
	Failed  bool   `json:"failed"`
	Detail  string `json:"detail,omitempty"`
}

// ArtifactSource supplies the relay binaries. Bundled is the default.
type ArtifactSource interface {
	Describe(arch string) (relaybin.Artifact, error)
	Lookup(arch string) (relaybin.Artifact, []byte, error)
}

// Bundled serves the relay binaries embedded in this Atlas build.
type Bundled struct{}

// Describe implements ArtifactSource.
func (Bundled) Describe(arch string) (relaybin.Artifact, error) { return relaybin.Describe(arch) }

// Lookup implements ArtifactSource.
func (Bundled) Lookup(arch string) (relaybin.Artifact, []byte, error) { return relaybin.Lookup(arch) }

// Resolver looks up the public hostname from the desktop.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// RunPreflight runs the read-only script and the DNS lookup.
func RunPreflight(ctx context.Context, r Runner, p config.RemoteProfile, resolver Resolver) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := r.Run(ctx, preflightScript, nil, nil)
	if err != nil {
		return Report{}, err
	}
	rep := ParseReport(res.Stdout)
	rep.At = time.Now()
	rep.RemoteIP = r.RemoteIP()
	rep.DNS = checkDNS(ctx, resolver, p, rep)
	return rep, nil
}

// ParseReport reads the preflight's key=value output. Unknown keys are
// ignored; values are already bounded by the script.
func ParseReport(out []byte) Report {
	rep := Report{Services: map[string]string{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "uname_s":
			rep.Kernel = value
		case "uname_m":
			rep.Machine = value
			rep.Arch = archOf(value)
		case "os_id":
			rep.OSID = strings.ToLower(value)
		case "os_like":
			rep.OSLike = strings.ToLower(value)
		case "os_version":
			rep.OSVersion = value
		case "os_name":
			rep.OSName = value
		case "sudo":
			rep.Privilege = value
		case "systemd":
			rep.Systemd = value == "yes"
		case "apt":
			rep.Apt = value == "yes"
		case "disk_kb":
			rep.DiskKB, _ = strconv.ParseInt(value, 10, 64)
		case "mem_kb":
			rep.MemKB, _ = strconv.ParseInt(value, 10, 64)
		case "addresses":
			rep.Addresses = strings.Fields(value)
		case "docker":
			rep.Docker = value == "yes"
		case "docker_version":
			rep.DockerVersion = value
		case "compose_version":
			rep.ComposeVersion = value
		case "tcp_listen":
			rep.PortsKnown = value != "unknown"
			if rep.PortsKnown {
				rep.TCPListen = strings.Fields(value)
			}
		case "udp_listen":
			rep.UDPListen = strings.Fields(value)
		case "ufw":
			rep.UFW = value
		case "ufw_rules":
			rep.UFWRules = strings.Fields(value)
		case "firewalld":
			rep.Firewalld = value
		case "installed":
			rep.Installed = value == "yes"
		case "managed":
			rep.Managed = value == "yes"
		case "state_b64":
			if data, err := base64.StdEncoding.DecodeString(value); err == nil && len(data) > 0 {
				var st RemoteState
				if json.Unmarshal(data, &st) == nil {
					rep.State = &st
				}
			}
		case "lock":
			rep.Lock = value
		case "binary_sha":
			rep.BinarySHA = hexOrEmpty(value)
		case "key_exists":
			rep.KeyExists = value == "yes"
		case "key_sha":
			rep.KeySHA = hexOrEmpty(value)
		case "backups":
			rep.Backups = strings.Fields(value)
		case "services":
			for f := range strings.FieldsSeq(value) {
				if svc, st, ok := strings.Cut(f, ":"); ok {
					rep.Services[svc] = st
				}
			}
		case "project_containers":
			rep.ProjectContainers = strings.Fields(value)
		case "done":
			rep.Complete = value == "yes"
		}
	}
	return rep
}

func hexOrEmpty(s string) string {
	if len(s) != 64 {
		return ""
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return s
}

func archOf(machine string) string {
	switch machine {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return ""
}

func checkDNS(ctx context.Context, resolver Resolver, p config.RemoteProfile, rep Report) DNSCheck {
	check := DNSCheck{Host: p.PublicHost}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := resolver.LookupIPAddr(ctx, p.PublicHost)
	if err != nil || len(addrs) == 0 {
		check.Failed = true
		check.Detail = "no A or AAAA record found"
		return check
	}
	server := map[netip.Addr]bool{}
	add := func(s string) {
		if a, err := netip.ParseAddr(s); err == nil {
			server[a.Unmap()] = true
		}
	}
	add(rep.RemoteIP)
	add(p.SSHHost)
	for _, a := range rep.Addresses {
		add(a)
	}
	for _, a := range addrs {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		check.Addresses = append(check.Addresses, ip.String())
		if server[ip] {
			check.Matches = true
		}
	}
	slices.Sort(check.Addresses)
	return check
}

// Assessment is the verdict on a report: whether installation can proceed,
// what blocks it and what the plan must include.
type Assessment struct {
	Blocking []Message
	Warnings []Message
	// InstallPackages lists distribution packages to install (Ubuntu only).
	InstallPackages []string
	// FirewallRules lists ufw rules to add.
	FirewallRules []string
	// ClearStaleLock is set when an abandoned installation lock exists.
	ClearStaleLock bool
	// ReplaceKey is set when the server holds a different connector key.
	ReplaceKey bool
	// HostChanged is set when the relay currently serves another hostname.
	HostChanged bool
	Artifact    relaybin.Artifact
}

// Ready reports whether nothing blocks the installation.
func (a Assessment) Ready() bool { return len(a.Blocking) == 0 }

// lockTTL is how long an installation lock is honored without progress.
const lockTTL = 30 * time.Minute

// Assess applies the supported-environment rules. localKeySHA is the hash
// of this desktop's connector key and token identifies this desktop's
// running or interrupted installation.
func Assess(rep Report, p config.RemoteProfile, localKeySHA, token string, now time.Time, artifacts ArtifactSource) Assessment {
	var a Assessment
	block := func(m Message) { a.Blocking = append(a.Blocking, m) }
	warn := func(m Message) { a.Warnings = append(a.Warnings, m) }

	if !rep.Complete {
		block(Msg("The server check did not finish; the connection may have dropped."))
		return a
	}
	if rep.Kernel != "Linux" {
		block(Msg("Only Linux servers are supported (found %s).", orUnknown(rep.Kernel)))
		return a
	}
	if rep.Arch == "" {
		block(Msg("Only amd64 and arm64 servers are supported (found %s).", orUnknown(rep.Machine)))
	} else if art, err := artifacts.Describe(rep.Arch); err != nil {
		block(Msg("This Atlas build does not include the relay for linux/%s; rebuild Atlas with go generate ./internal/remotecontrol/relaybin.", rep.Arch))
	} else {
		a.Artifact = art
	}

	family := rep.OSID
	switch family {
	case "ubuntu":
		if versionLess(rep.OSVersion, "22.04") {
			block(Msg("Ubuntu %s is not supported; use Ubuntu 22.04 or newer.", rep.OSVersion))
		}
	case "debian":
		if versionLess(rep.OSVersion, "12") {
			block(Msg("Debian %s is not supported; use Debian 12 or newer.", rep.OSVersion))
		}
	default:
		block(Msg("%s is not supported; /rc supports Ubuntu 22.04+ and Debian 12+.", orUnknown(rep.OSName)))
	}

	if rep.Privilege != "root" && rep.Privilege != "passwordless" {
		block(Msg("The SSH user needs root or passwordless sudo to install services."))
		return a
	}

	composeOK := rep.Docker && strings.HasPrefix(strings.TrimPrefix(rep.ComposeVersion, "v"), "2")
	if !composeOK {
		switch {
		case family == "ubuntu" && rep.Apt && rep.Systemd:
			if !rep.Docker {
				a.InstallPackages = append(a.InstallPackages, "docker.io")
			}
			a.InstallPackages = append(a.InstallPackages, "docker-compose-v2")
		case !rep.Docker:
			block(Msg("Docker is not installed. Install Docker Engine with the Compose plugin from Docker's documentation, then test again."))
		default:
			block(Msg("Docker Compose v2 is missing. Install the Docker Compose plugin, then test again."))
		}
	}

	if rep.DiskKB > 0 && rep.DiskKB < 1<<20 {
		block(Msg("Less than 1 GiB of disk space is free."))
	}
	if rep.MemKB > 0 && rep.MemKB < 400<<10 {
		warn(Msg("The server has less than 400 MiB of memory."))
	}

	if rep.Installed && !rep.Managed {
		block(Msg("%s exists but was not created by /rc; Atlas will not overwrite it.", RemoteDir))
	}
	ours := len(rep.ProjectContainers) > 0
	if !rep.PortsKnown {
		warn(Msg("Could not list listening ports; ports 80 and 443 must be free."))
	} else if !ours {
		for _, port := range []string{"80", "443"} {
			if portInUse(rep.TCPListen, port) {
				block(Msg("Port %s is already used by another service; Atlas will not stop it.", port))
			}
		}
	}

	switch rep.UFW {
	case "active":
		for _, rule := range []string{"80/tcp", "443/tcp", "443/udp"} {
			if !slices.Contains(rep.UFWRules, rule) && !slices.Contains(rep.UFWRules, strings.Split(rule, "/")[0]) {
				a.FirewallRules = append(a.FirewallRules, rule)
			}
		}
	}
	if rep.Firewalld == "active" {
		warn(Msg("firewalld is active; allow HTTP and HTTPS there yourself. Atlas does not change firewalld."))
	}
	warn(Msg("Your provider's cloud firewall must also allow TCP 80 and 443; Atlas cannot see it."))

	if rep.Lock != "" {
		owner, ts, _ := strings.Cut(rep.Lock, " ")
		started, _ := strconv.ParseInt(ts, 10, 64)
		age := now.Sub(time.Unix(started, 0))
		switch {
		case owner == token && token != "":
		case started > 0 && age < lockTTL:
			block(Msg("Another installation is in progress on this server; wait for it or test again in %d minutes.", int((lockTTL-age).Minutes())+1))
		default:
			a.ClearStaleLock = true
			warn(Msg("An abandoned installation lock will be cleared."))
		}
	}

	if rep.KeyExists && rep.KeySHA != localKeySHA {
		a.ReplaceKey = true
		warn(Msg("The server holds a different connector key; replacing it disconnects any other computer using this relay."))
	}
	if rep.State != nil && rep.State.PublicHost != "" && rep.State.PublicHost != p.PublicHost {
		a.HostChanged = true
		warn(Msg("The relay currently serves %s; switching to %s means phones must pair again.", rep.State.PublicHost, p.PublicHost))
	}

	switch {
	case rep.DNS.Failed:
		block(Msg("%s has no DNS A/AAAA record yet. Point it to %s, wait for DNS to update, then test again.", p.PublicHost, orUnknown(rep.RemoteIP)))
	case !rep.DNS.Matches:
		warn(Msg("%s resolves to %s, not to this server; HTTPS certificate issuance will fail unless a proxy forwards to it.", p.PublicHost, strings.Join(rep.DNS.Addresses, ", ")))
	}
	return a
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func portInUse(listen []string, port string) bool {
	for _, l := range listen {
		if i := strings.LastIndexByte(l, ':'); i >= 0 && l[i+1:] == port {
			return true
		}
	}
	return false
}

// versionLess compares dotted numeric versions; unparsable versions count
// as too old so an unknown system is never treated as supported.
func versionLess(v, minimum string) bool {
	parse := func(s string) []int {
		var out []int
		for part := range strings.SplitSeq(s, ".") {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil
			}
			out = append(out, n)
		}
		return out
	}
	a, b := parse(v), parse(minimum)
	if a == nil {
		return true
	}
	for i := range max(len(a), len(b)) {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}
