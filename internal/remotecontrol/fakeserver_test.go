package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol/relaybin"
)

func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), d)
}

// fakeServer models the effects of the deployment scripts on a server. It
// recognizes each fixed script and applies what that script does, so the
// orchestration (ordering, idempotency, locking, rollback, journaling) is
// tested without a real VPS.
type fakeServer struct {
	mu sync.Mutex

	osID, osVersion, osName, machine, privilege string
	docker                                      bool
	compose                                     string
	tcpListen                                   []string
	ufw                                         string
	ufwRules                                    []string
	addresses                                   []string

	files    map[string][]byte
	lock     string
	services map[string]string
	backups  map[string]map[string][]byte

	// badBinary makes the relay crash-loop while this binary is installed.
	badBinary string
	// failScript makes one script exit 1.
	failScript string
	// blockScript blocks that script until the context ends.
	blockScript string

	log      []string
	restarts [][]string
	ups      int
	packages []string
	firewall []string
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		osID: "ubuntu", osVersion: "24.04", osName: "Ubuntu 24.04 LTS", machine: "x86_64", privilege: "passwordless",
		docker: true, compose: "2.27.0", ufw: "inactive", addresses: []string{"203.0.113.10"},
		files: map[string][]byte{}, services: map[string]string{}, backups: map[string]map[string][]byte{},
	}
}

func (f *fakeServer) Close() error     { return nil }
func (f *fakeServer) RemoteIP() string { return "203.0.113.10" }

func (f *fakeServer) installed() bool { return f.files[RemoteDir+"/.atlas-rc"] != nil }

func (f *fakeServer) name(script string) string {
	for name, s := range map[string]string{
		"preflight": preflightScript, "lock": lockScript, "unlock": unlockScript, "packages": packagesScript,
		"firewall": firewallScript, "backup": backupScript, "upload": uploadScript, "start": startScript,
		"ps": psScript, "logs": logsScript, "rollback": rollbackScript, "remove": removeScript,
	} {
		if s == script {
			return name
		}
	}
	return "unknown"
}

func (f *fakeServer) Run(ctx context.Context, script string, args []string, stdin []byte) (Result, error) {
	name := f.name(script)
	f.mu.Lock()
	f.log = append(f.log, name)
	block, fail := f.blockScript == name, f.failScript == name
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return Result{Exit: -1}, newError("canceled", Msg("Canceled."), Message{})
	}
	if fail {
		return Result{Stdout: []byte("simulated failure\n"), Exit: 1}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out, code := f.apply(name, args, stdin)
	return Result{Stdout: []byte(out), Exit: code}, nil
}

func (f *fakeServer) healthy() {
	f.services["caddy"] = "running"
	f.services["relay"] = "running"
	if f.badBinary != "" && sha256Hex(f.files[RemoteDir+"/bin/atlas-mobile-relay"]) == f.badBinary {
		f.services["relay"] = "restarting"
	}
}

func (f *fakeServer) apply(name string, args []string, stdin []byte) (string, int) {
	switch name {
	case "preflight":
		return f.preflight(), 0
	case "lock":
		if f.lock != "" && !strings.HasPrefix(f.lock, args[0]+" ") {
			return "locked\n", 3
		}
		f.lock = args[0] + " " + fmt.Sprint(time.Now().Unix())
		f.files[RemoteDir+"/.atlas-rc"] = []byte("Managed by Atlas /rc\n")
		return "ok\n", 0
	case "unlock":
		if strings.HasPrefix(f.lock, args[0]+" ") {
			f.lock = ""
		}
		return "ok\n", 0
	case "packages":
		f.packages = append(f.packages, args...)
		f.docker, f.compose = true, "2.24.6"
		return "ok\n", 0
	case "firewall":
		f.firewall = append(f.firewall, args...)
		f.ufwRules = append(f.ufwRules, args...)
		return "ok\n", 0
	case "backup":
		b := map[string][]byte{}
		for _, n := range []string{"compose.yaml", "Caddyfile", ".env", "state.json", "bin/atlas-mobile-relay"} {
			if data, ok := f.files[RemoteDir+"/"+n]; ok {
				b[n] = slices.Clone(data)
			}
		}
		f.backups[args[0]] = b
		return "ok\n", 0
	case "upload":
		dest, want := args[0], args[1]
		if sha256Hex(stdin) != want {
			return "mismatch\n", 4
		}
		if string(f.files[dest]) == string(stdin) {
			return "unchanged\n", 0
		}
		f.files[dest] = slices.Clone(stdin)
		return "changed\n", 0
	case "start":
		f.ups++
		f.restarts = append(f.restarts, slices.Clone(args))
		f.healthy()
		return "ok\n", 0
	case "ps":
		var b strings.Builder
		for _, s := range slices.Sorted(maps.Keys(f.services)) {
			b.WriteString(s + ":" + f.services[s] + "\n")
		}
		return b.String(), 0
	case "logs":
		return "relay-1 | Phone relay listening\n", 0
	case "rollback":
		b, ok := f.backups[args[0]]
		if !ok {
			return "missing\n", 5
		}
		for n, data := range b {
			f.files[RemoteDir+"/"+n] = slices.Clone(data)
		}
		f.healthy()
		return "ok\n", 0
	case "remove":
		for k := range f.files {
			if strings.HasPrefix(k, RemoteDir) || k == RemoteKeyFile {
				delete(f.files, k)
			}
		}
		f.services = map[string]string{}
		return "ok\n", 0
	}
	return "unknown script\n", 127
}

func (f *fakeServer) preflight() string {
	var b strings.Builder
	kv := func(k, v string) { b.WriteString(k + "=" + v + "\n") }
	kv("uname_s", "Linux")
	kv("uname_m", f.machine)
	kv("os_id", f.osID)
	kv("os_version", f.osVersion)
	kv("os_name", f.osName)
	kv("sudo", f.privilege)
	kv("systemd", "yes")
	kv("apt", "yes")
	kv("disk_kb", "20000000")
	kv("mem_kb", "1000000")
	kv("addresses", strings.Join(f.addresses, " "))
	if f.docker {
		kv("docker", "yes")
		kv("docker_version", "27.0.3")
		kv("compose_version", f.compose)
	} else {
		kv("docker", "no")
	}
	kv("tcp_listen", strings.Join(f.tcpListen, " "))
	kv("ufw", f.ufw)
	kv("ufw_rules", strings.Join(f.ufwRules, " "))
	if f.files[RemoteDir+"/.atlas-rc"] != nil || f.files[RemoteDir+"/foreign"] != nil {
		kv("installed", "yes")
		kv("managed", map[bool]string{true: "yes", false: "no"}[f.installed()])
		if st := f.files[RemoteDir+"/state.json"]; st != nil {
			kv("state_b64", base64.StdEncoding.EncodeToString(st))
		}
		kv("lock", f.lock)
		if bin := f.files[RemoteDir+"/bin/atlas-mobile-relay"]; bin != nil {
			kv("binary_sha", sha256Hex(bin))
		}
		kv("backups", strings.Join(slices.Sorted(maps.Keys(f.backups)), " "))
		var svc []string
		for _, s := range slices.Sorted(maps.Keys(f.services)) {
			svc = append(svc, s+":"+f.services[s])
		}
		kv("services", strings.Join(svc, " "))
	} else {
		kv("installed", "no")
	}
	if key := f.files[RemoteKeyFile]; key != nil {
		kv("key_exists", "yes")
		kv("key_sha", sha256Hex(key))
	} else {
		kv("key_exists", "no")
	}
	if len(f.services) > 0 {
		kv("project_containers", "atlas-mobile-relay-caddy-1 atlas-mobile-relay-relay-1")
	}
	kv("done", "yes")
	return b.String()
}

func (f *fakeServer) state() RemoteState {
	f.mu.Lock()
	defer f.mu.Unlock()
	var st RemoteState
	_ = json.Unmarshal(f.files[RemoteDir+"/state.json"], &st)
	return st
}

type fakeDialer struct {
	server *fakeServer
	err    error
	dials  int
	mu     sync.Mutex
}

func (d *fakeDialer) Dial(context.Context, config.RemoteProfile, []byte) (Runner, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials++
	if d.err != nil {
		return nil, d.err
	}
	return d.server, nil
}

type fakeApprover struct {
	mu       sync.Mutex
	answer   bool
	requests []ApprovalRequest
}

func (a *fakeApprover) Approve(_ context.Context, req ApprovalRequest) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, req)
	return a.answer, nil
}

type fakeChecker struct {
	mu    sync.Mutex
	https string
	link  string
	calls int
}

func (c *fakeChecker) HTTPS(context.Context, string) Check {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return Check{State: c.https, Detail: Msg("HTTPS works and the phone panel answers.")}
}

func (c *fakeChecker) Link(context.Context, string, string, bool) Check {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Check{State: c.link, Detail: Msg("The relay forwards phone requests to this computer.")}
}

type fakeArtifacts struct{ data map[string][]byte }

func (a fakeArtifacts) Describe(arch string) (relaybin.Artifact, error) {
	data, ok := a.data[arch]
	if !ok {
		return relaybin.Artifact{}, relaybin.ErrNotBundled
	}
	return relaybin.Artifact{Arch: arch, SHA256: sha256Hex(data), Size: int64(len(data)), Version: "v-test"}, nil
}

func (a fakeArtifacts) Lookup(arch string) (relaybin.Artifact, []byte, error) {
	art, err := a.Describe(arch)
	return art, a.data[arch], err
}

type staticResolver map[string][]string

func (r staticResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	var out []net.IPAddr
	for _, a := range r[host] {
		out = append(out, net.IPAddr{IP: net.ParseIP(a)})
	}
	if len(out) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return out, nil
}
