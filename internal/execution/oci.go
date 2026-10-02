package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/google/uuid"
)

const outputLimit = 2 * 1024 * 1024

type ociRunner struct {
	policy          ExecutionPolicy
	store           *engineering.Store
	owner           string
	mu              sync.Mutex
	cancel          map[string]context.CancelFunc
	commandOverride func(context.Context, io.Reader, ...string) ([]byte, []byte, error)
}

type runRecord struct {
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	Root       string                  `json:"root"`
	TaskID     string                  `json:"task_id,omitempty"`
	SessionID  string                  `json:"session_id,omitempty"`
	Policy     ExecutionPolicy         `json:"policy"`
	MaskRef    engineering.ArtifactRef `json:"mask_ref"`
	Result     Result                  `json:"result"`
	StartedAt  int64                   `json:"started_at"`
	TimeoutMS  int64                   `json:"timeout_ms"`
}

type boundedBuffer struct {
	bytes.Buffer
	exceeded bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	n := len(data)
	if b.Len()+n > outputLimit {
		b.exceeded = true
		data = data[:max(0, outputLimit-b.Len())]
	}
	_, _ = b.Buffer.Write(data)
	return n, nil
}

func NewRunner(ctx context.Context, policy ExecutionPolicy, store *engineering.Store) (Runner, error) {
	p := defaults(policy)
	if err := ValidatePolicy(ctx, p); err != nil {
		return nil, err
	}
	if p.Mode == "legacy" {
		return nil, nil
	}
	if store == nil {
		return nil, fmt.Errorf("isolated execution requires a durable store")
	}
	storePath := filepath.Clean(store.Dir())
	if runtime.GOOS == "windows" {
		storePath = strings.ToLower(storePath)
	}
	r := &ociRunner{policy: p, store: store, owner: engineering.Hash(storePath), cancel: map[string]context.CancelFunc{}}
	data, _, err := r.command(ctx, nil, "image", "inspect", p.Image)
	if err != nil {
		return nil, fmt.Errorf("configured local image unavailable: %w", err)
	}
	var images []struct {
		OS          string   `json:"Os"`
		RepoDigests []string `json:"RepoDigests"`
		Config      struct {
			Volumes map[string]any `json:"Volumes"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(data, &images); err != nil || len(images) != 1 || images[0].OS != "linux" {
		return nil, fmt.Errorf("execution requires an inspected Linux image")
	}
	found := false
	for _, digest := range images[0].RepoDigests {
		if digest == p.Image {
			found = true
		}
	}
	if !found || len(images[0].Config.Volumes) != 0 {
		return nil, fmt.Errorf("image digest or implicit volume policy mismatch")
	}
	return r, nil
}

func (r *ociRunner) command(ctx context.Context, input io.Reader, args ...string) ([]byte, []byte, error) {
	if r.commandOverride != nil {
		return r.commandOverride(ctx, input, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.policy.RuntimePath, args...)
	cmd.Stdin = input
	cmd.Env = runtimeEnvironment()
	cmd.WaitDelay = time.Second
	var output, diagnostic boundedBuffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	err := cmd.Run()
	if output.exceeded || diagnostic.exceeded {
		return nil, nil, fmt.Errorf("runtime response exceeds size limit")
	}
	return output.Bytes(), diagnostic.Bytes(), err
}

func runtimeEnvironment() []string {
	result := []string{}
	for _, key := range []string{"PATH", "Path", "SYSTEMROOT", "SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value, ok := os.LookupEnv(key); ok {
			result = append(result, key+"="+value)
		}
	}
	return result
}

func validateRoot(root string) error {
	if !filepath.IsAbs(root) || strings.ContainsAny(root, ",\x00\r\n") {
		return fmt.Errorf("execution mount requires a literal absolute project root")
	}
	root = filepath.Clean(root)
	if root == filepath.VolumeName(root)+string(filepath.Separator) {
		return fmt.Errorf("filesystem root cannot be mounted")
	}
	home, _ := os.UserHomeDir()
	if strings.EqualFold(root, filepath.Clean(home)) {
		return fmt.Errorf("host home cannot be mounted")
	}
	for current := root; current != filepath.Dir(current); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("execution root is unavailable or traverses a symlink")
		}
	}
	return nil
}

func (r *ociRunner) createArguments(req Request) ([]string, error) {
	if err := validateRoot(req.Root); err != nil {
		return nil, err
	}
	if len(req.Argv) < 1 || len(req.Argv) > 128 || len(req.Argv[0]) == 0 || req.Argv[0][0] != '/' {
		return nil, fmt.Errorf("container command requires an absolute executable")
	}
	for _, arg := range req.Argv {
		if len(arg) > 256*1024 || strings.ContainsRune(arg, 0) {
			return nil, fmt.Errorf("invalid command argument")
		}
	}
	mount := "type=bind,source=" + filepath.Clean(req.Root) + ",target=/workspace"
	if r.policy.ReadOnly {
		mount += ",readonly"
	}
	network := "none"
	if r.policy.Network == "unrestricted" {
		network = "bridge"
	}
	args := []string{"container", "create", "--pull=never", "--name", "atlas-" + req.RunID, "--label", "atlas.owner=" + r.owner, "--label", "atlas.run=" + req.RunID, "--network", network, "--cpus", strconv.FormatFloat(r.policy.CPUs, 'f', -1, 64), "--memory", strconv.FormatInt(r.policy.MemoryBytes, 10), "--memory-swap", strconv.FormatInt(r.policy.MemoryBytes, 10), "--pids-limit", strconv.Itoa(r.policy.MaxProcesses), "--cap-drop=ALL", "--security-opt", "no-new-privileges", "--no-healthcheck", "--mount", mount, "--workdir", "/workspace", "--interactive"}
	if r.policy.ReadOnly {
		args = append(args, "--read-only")
	}
	// Temporary compilation output is writable without changing project sources.
	args = append(args, "--tmpfs", "/tmp:rw,nosuid,nodev,size="+strconv.FormatInt(r.policy.MemoryBytes/2, 10))
	for _, value := range forwardedEnvironment(r.policy, req.Env) {
		args = append(args, "--env", value)
	}
	cargoHome := "/workspace/.atlas-env/cargo"
	argv := req.Argv
	if r.policy.ReadOnly {
		cargoHome = "/tmp/atlas-cargo"
		// Cargo locks and metadata need writable cache state. Copy only the
		// already masked project cache into memory-bounded temporary storage.
		argv = append([]string{"/bin/sh", "-c", `set -eu
if [ -d /workspace/.atlas-env/cargo ]; then
  mkdir -p /tmp/atlas-cargo
  cp -R /workspace/.atlas-env/cargo/. /tmp/atlas-cargo/
  chmod -R u+rwX /tmp/atlas-cargo
fi
exec "$@"`, "atlas-cache-stage"}, req.Argv...)
	}
	for _, value := range []string{"GOMODCACHE=/workspace/.atlas-env/go/mod", "GOCACHE=/tmp/atlas-go-build", "GOTOOLCHAIN=local", "CARGO_HOME=" + cargoHome, "CARGO_TARGET_DIR=/tmp/atlas-cargo-target", "TMPDIR=/tmp"} {
		args = append(args, "--env", value)
	}
	args = append(args, "--entrypoint", argv[0], r.policy.Image)
	return append(args, argv[1:]...), nil
}

func (r *ociRunner) persist(ctx context.Context, record runRecord, expected uint64) error {
	if _, err := uuid.Parse(record.Result.RunID); err != nil {
		return fmt.Errorf("invalid execution record identity")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var linked []engineering.ArtifactRef
	if record.MaskRef.Hash != "" {
		linked = append(linked, record.MaskRef)
	}
	if record.Result.OutputRef.Hash != "" {
		linked = append(linked, record.Result.OutputRef)
	}
	if record.Result.ErrorRef.Hash != "" {
		linked = append(linked, record.Result.ErrorRef)
	}
	_, err = r.store.PutRecord(ctx, "execution-runs-"+strings.ToLower(record.Result.RunID[:2]), record.Result.RunID, expected, data, linked...)
	return err
}

func (r *ociRunner) load(ctx context.Context, id string) (engineering.Record, runRecord, error) {
	if _, err := uuid.Parse(id); err != nil {
		return engineering.Record{}, runRecord{}, fmt.Errorf("invalid execution run identity")
	}
	ref, data, err := r.store.ReadRecord(ctx, "execution-runs-"+strings.ToLower(id[:2]), id)
	var record runRecord
	if err == nil {
		err = json.Unmarshal(data, &record)
	}
	if err == nil && record.Result.RunID != id {
		err = fmt.Errorf("execution record identity mismatch")
	}
	if err == nil && record.Policy.Mode != "" && !strings.EqualFold(record.Policy.RuntimePath, r.policy.RuntimePath) {
		err = fmt.Errorf("execution runtime differs from the recorded backend")
	}
	return ref, record, err
}

var containerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (r *ociRunner) Start(ctx context.Context, req Request) (RunHandle, error) {
	if req.RunID == "" {
		req.RunID = uuid.NewString()
	}
	if _, err := uuid.Parse(req.RunID); err != nil {
		return RunHandle{}, fmt.Errorf("invalid run ID")
	}
	args, err := r.createArguments(req)
	if err != nil {
		return RunHandle{}, err
	}
	maskArgs, maskRef, err := r.privateMounts(ctx, req.Root)
	if err != nil {
		return RunHandle{}, err
	}
	for i, arg := range args {
		if arg == "--entrypoint" {
			tail := append([]string(nil), args[i:]...)
			args = append(append(args[:i:i], maskArgs...), tail...)
			break
		}
	}
	record := runRecord{Result: Result{RunID: req.RunID, Backend: "oci", HostOS: runtime.GOOS, ExecutionOS: "linux", Status: "preparing"}, StartedAt: time.Now().UnixMilli(), TimeoutMS: r.policy.TimeoutMS}
	record.MaskRef = maskRef
	record.Root, record.TaskID, record.SessionID, record.Policy = req.Root, req.TaskID, req.SessionID, r.policy
	record.ToolCallID = req.ToolCallID
	if err := r.persist(ctx, record, 0); err != nil {
		return RunHandle{}, err
	}
	data, _, err := r.command(ctx, nil, args...)
	if err != nil {
		return RunHandle{RunID: req.RunID, Backend: "oci"}, fmt.Errorf("container creation failed: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if !containerID.MatchString(id) {
		return RunHandle{RunID: req.RunID, Backend: "oci"}, fmt.Errorf("runtime returned an invalid container identity")
	}
	record.Result.ContainerID, record.Result.Status = id, "running"
	if _, err := r.inspect(ctx, req.RunID, record); err != nil {
		return RunHandle{RunID: req.RunID, Backend: "oci", ContainerID: id}, err
	}
	if err := r.persist(ctx, record, 1); err != nil {
		return RunHandle{RunID: req.RunID, Backend: "oci", ContainerID: id}, err
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(r.policy.TimeoutMS)*time.Millisecond)
	r.mu.Lock()
	r.cancel[req.RunID] = cancel
	r.mu.Unlock()
	go r.attach(runCtx, req, id, cancel)
	return RunHandle{RunID: req.RunID, Backend: "oci", ContainerID: id}, nil
}

func (r *ociRunner) attach(ctx context.Context, req Request, id string, cancel context.CancelFunc) {
	defer cancel()
	cmd := exec.CommandContext(ctx, r.policy.RuntimePath, "container", "start", "--attach", "--interactive", id)
	cmd.Env, cmd.Stdin, cmd.WaitDelay = runtimeEnvironment(), req.Stdin, time.Second
	var out, diagnostic boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	if ctx.Err() != nil {
		_ = r.Cancel(cleanup, req.RunID)
	}
	ref, record, readErr := r.load(cleanup, req.RunID)
	if readErr == nil {
		record.Result.OutputRef, readErr = r.store.PutArtifact(cleanup, "execution-output", out.Bytes())
		if readErr == nil {
			record.Result.ErrorRef, readErr = r.store.PutArtifact(cleanup, "execution-error", diagnostic.Bytes())
		}
		if readErr == nil {
			record.Result.OutputHash, record.Result.ErrorHash = record.Result.OutputRef.Hash, record.Result.ErrorRef.Hash
			observed, inspectErr := r.inspect(cleanup, req.RunID, record)
			if inspectErr == nil && !observed.State.Running && (observed.State.Status == "exited" || observed.State.Status == "dead") {
				record.Result.Done, record.Result.ExitCode = true, &observed.State.ExitCode
				record.Result.Status = "exited"
			} else if err != nil {
				record.Result.Status = "runtime-error"
			}
			if ctx.Err() != nil {
				record.Result.Status = "cancelled"
			}
			if out.exceeded || diagnostic.exceeded {
				record.Result.Status = "output-truncated"
			}
			if r.persist(cleanup, record, ref.Revision) == nil && record.Result.Done {
				_, _, _ = r.command(cleanup, nil, "container", "rm", id)
			}
		}
	}
	r.mu.Lock()
	delete(r.cancel, req.RunID)
	r.mu.Unlock()
}

type inspectedContainer struct {
	ID     string `json:"Id"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status   string `json:"Status"`
		Running  bool   `json:"Running"`
		ExitCode int    `json:"ExitCode"`
	} `json:"State"`
}

func (r *ociRunner) inspect(ctx context.Context, id string, record runRecord) (inspectedContainer, error) {
	identity := record.Result.ContainerID
	if identity == "" {
		identity = "atlas-" + id
	}
	data, _, err := r.command(ctx, nil, "container", "inspect", identity)
	if err != nil {
		return inspectedContainer{}, err
	}
	var containers []inspectedContainer
	if err := json.Unmarshal(data, &containers); err != nil || len(containers) != 1 {
		return inspectedContainer{}, fmt.Errorf("invalid runtime inspection")
	}
	container := containers[0]
	if !containerID.MatchString(container.ID) || container.Config.Labels["atlas.owner"] != r.owner || container.Config.Labels["atlas.run"] != id || record.Result.ContainerID != "" && record.Result.ContainerID != container.ID {
		return inspectedContainer{}, fmt.Errorf("container ownership or identity mismatch")
	}
	return container, nil
}

func (r *ociRunner) Observe(ctx context.Context, id string) (Result, error) {
	ref, record, err := r.load(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if record.Result.Done {
		return record.Result, nil
	}
	if record.Result.Status == "runtime-error" {
		return record.Result, fmt.Errorf("container attachment failed")
	}
	container, err := r.inspect(ctx, id, record)
	if err != nil {
		record.Result.Status = "unknown"
		record.Result.Done = false
		return record.Result, err
	}
	record.Result.ContainerID = container.ID
	if !container.State.Running && (container.State.Status == "exited" || container.State.Status == "dead") {
		r.mu.Lock()
		_, attaching := r.cancel[id]
		r.mu.Unlock()
		if attaching {
			return record.Result, nil
		}
		record.Result.Done, record.Result.ExitCode = true, &container.State.ExitCode
		if record.Result.Status == "running" {
			record.Result.Status = "exited"
		}
		output, diagnostic, err := r.command(ctx, nil, "container", "logs", container.ID)
		if err != nil {
			record.Result.Done = false
			return record.Result, err
		}
		record.Result.OutputRef, err = r.store.PutArtifact(ctx, "execution-output", output)
		if err != nil {
			return Result{}, err
		}
		record.Result.ErrorRef, err = r.store.PutArtifact(ctx, "execution-error", diagnostic)
		if err != nil {
			return Result{}, err
		}
		record.Result.OutputHash, record.Result.ErrorHash = record.Result.OutputRef.Hash, record.Result.ErrorRef.Hash
		if err := r.persist(ctx, record, ref.Revision); err != nil {
			return Result{}, err
		}
		_, _, _ = r.command(ctx, nil, "container", "rm", container.ID)
	}
	if container.State.Running && time.Now().UnixMilli()-record.StartedAt > record.TimeoutMS {
		return record.Result, r.Cancel(ctx, id)
	}
	return record.Result, nil
}

func (r *ociRunner) Cancel(ctx context.Context, id string) error {
	_, record, err := r.load(ctx, id)
	if err != nil {
		return err
	}
	if record.Result.Done {
		return nil
	}
	container, err := r.inspect(ctx, id, record)
	if err != nil {
		return err
	}
	if container.State.Running {
		if _, _, err := r.command(ctx, nil, "container", "kill", container.ID); err != nil {
			return err
		}
	}
	if _, _, err := r.command(ctx, nil, "container", "rm", "--force", container.ID); err != nil {
		_, current, readErr := r.load(ctx, id)
		if readErr == nil && current.Result.Done {
			return nil
		}
		return err
	}
	r.mu.Lock()
	cancel := r.cancel[id]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	ref, record, err := r.load(ctx, id)
	if err != nil {
		return err
	}
	if record.Result.Done {
		return nil
	}
	if record.Result.OutputRef.Hash == "" {
		record.Result.OutputRef, err = r.store.PutArtifact(ctx, "execution-output", nil)
		if err != nil {
			return err
		}
	}
	if record.Result.ErrorRef.Hash == "" {
		record.Result.ErrorRef, err = r.store.PutArtifact(ctx, "execution-error", nil)
		if err != nil {
			return err
		}
	}
	record.Result.Done, record.Result.ExitCode, record.Result.Status = true, nil, "cancelled"
	record.Result.OutputHash, record.Result.ErrorHash = record.Result.OutputRef.Hash, record.Result.ErrorRef.Hash
	return r.persist(ctx, record, ref.Revision)
}

func (r *ociRunner) Run(ctx context.Context, req Request) (Result, error) {
	handle, err := r.Start(ctx, req)
	if err != nil {
		return Result{RunID: handle.RunID, Backend: "oci", Status: "unavailable"}, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := r.Cancel(cleanup, handle.RunID)
			cancel()
			return Result{RunID: handle.RunID, Backend: "oci", Status: "cancelled"}, errors.Join(ctx.Err(), err)
		case <-ticker.C:
			result, err := r.Observe(ctx, handle.RunID)
			if err != nil || result.Done {
				return result, err
			}
		}
	}
}
