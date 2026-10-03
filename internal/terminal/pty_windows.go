package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"golang.org/x/sys/windows"
)

type windowsStreams struct{ input, output *os.File }

func (s windowsStreams) Read(data []byte) (int, error)  { return s.output.Read(data) }
func (s windowsStreams) Write(data []byte) (int, error) { return s.input.Write(data) }
func (s windowsStreams) Close() error                   { _ = s.input.Close(); return s.output.Close() }

var _ io.ReadWriteCloser = windowsStreams{}

func startProcess(req execution.Request, size execution.TerminalSize) (processTerminal, error) {
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		return processTerminal{}, err
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		_ = inputRead.Close()
		_ = inputWrite.Close()
		return processTerminal{}, err
	}
	var console windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: int16(size.Width), Y: int16(size.Height)}, windows.Handle(inputRead.Fd()), windows.Handle(outputWrite.Fd()), 0, &console); err != nil {
		_ = inputRead.Close()
		_ = inputWrite.Close()
		_ = outputRead.Close()
		_ = outputWrite.Close()
		return processTerminal{}, fmt.Errorf("%w: ConPTY: %v", ErrUnavailable, err)
	}
	cleanup := true
	defer func() {
		_ = inputRead.Close()
		_ = outputWrite.Close()
		if cleanup {
			// Close the reader before closing a failed, unattached console so
			// final frame output cannot deadlock its synchronous pipe.
			_ = inputWrite.Close()
			_ = outputRead.Close()
			windows.ClosePseudoConsole(console)
		}
	}()
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return processTerminal{}, err
	}
	defer attributes.Delete()
	//nolint:govet // Windows expects HPCON as lpValue, not its address.
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(console), unsafe.Sizeof(console)); err != nil {
		return processTerminal{}, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return processTerminal{}, err
	}
	defer func() {
		if cleanup {
			_ = windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return processTerminal{}, err
	}
	executable := req.Argv[0]
	if strings.ContainsAny(executable, `/\`) && !filepath.IsAbs(executable) {
		executable = filepath.Join(req.Root, executable)
	}
	executable, err = exec.LookPath(executable)
	if err != nil {
		return processTerminal{}, err
	}
	app, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return processTerminal{}, err
	}
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{executable}, req.Argv[1:]...)))
	if err != nil {
		return processTerminal{}, err
	}
	root, err := windows.UTF16PtrFromString(req.Root)
	if err != nil {
		return processTerminal{}, err
	}
	// Windows environment keys are case insensitive. Last supplied value
	// wins, including the automation marker added by Start.
	envMap := map[string]string{}
	for _, value := range req.Env {
		key, _, ok := strings.Cut(value, "=")
		if !ok || key == "" || strings.ContainsRune(value, 0) {
			continue
		}
		envMap[strings.ToUpper(key)] = value
	}
	envValues := make([]string, 0, len(envMap))
	for _, value := range envMap {
		envValues = append(envValues, value)
	}
	sort.Slice(envValues, func(i, j int) bool { return strings.ToUpper(envValues[i]) < strings.ToUpper(envValues[j]) })
	var environment []uint16
	for _, value := range envValues {
		entry, conversionErr := windows.UTF16FromString(value)
		if conversionErr != nil {
			return processTerminal{}, conversionErr
		}
		environment = append(environment, entry...)
	}
	environment = append(environment, 0)
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	// Clear inherited standard handles; the child must obtain its handles
	// from ConPTY instead of the parent's redirected CLI streams.
	startup.Flags = windows.STARTF_USESTDHANDLES
	var process windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(app, command, nil, nil, false, flags, &environment[0], root, &startup.StartupInfo, &process); err != nil {
		return processTerminal{}, err
	}
	if err := windows.AssignProcessToJobObject(job, process.Process); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		_ = windows.CloseHandle(process.Thread)
		_ = windows.CloseHandle(process.Process)
		return processTerminal{}, err
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(process.Thread)
		_ = windows.CloseHandle(process.Process)
		return processTerminal{}, err
	}
	_ = windows.CloseHandle(process.Thread)
	runtime.KeepAlive(attributes)
	runtime.KeepAlive(environment)
	cleanup = false
	var handleMu sync.Mutex
	closed := false
	return processTerminal{
		stream: windowsStreams{inputWrite, outputRead},
		resize: func(size execution.TerminalSize) error {
			handleMu.Lock()
			defer handleMu.Unlock()
			if closed {
				return io.ErrClosedPipe
			}
			return windows.ResizePseudoConsole(console, windows.Coord{X: int16(size.Width), Y: int16(size.Height)})
		},
		kill: func() error {
			handleMu.Lock()
			defer handleMu.Unlock()
			if closed {
				return nil
			}
			return windows.TerminateJobObject(job, 1)
		},
		wait: func() (int, error) {
			if _, err := windows.WaitForSingleObject(process.Process, windows.INFINITE); err != nil {
				return -1, err
			}
			var code uint32
			err := windows.GetExitCodeProcess(process.Process, &code)
			return int(code), err
		},
		finish: func() {
			handleMu.Lock()
			defer handleMu.Unlock()
			closed = true
			_ = windows.TerminateJobObject(job, 1)
			windows.ClosePseudoConsole(console)
			_ = windows.CloseHandle(process.Process)
			_ = windows.CloseHandle(job)
			_ = inputWrite.Close()
		},
	}, nil
}
