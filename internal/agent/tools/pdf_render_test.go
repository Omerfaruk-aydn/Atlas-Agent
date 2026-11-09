package tools

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestPDFRenderArgsSelectOnePage(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		[]string{"-png", "-singlefile", "-r", "110", "-f", "3", "-l", "3", "in.pdf", "out"},
		pdfRenderArgs("in.pdf", "out", 3, 110))
}

func TestSanitizePDFStemKeepsNamesSafe(t *testing.T) {
	t.Parallel()
	require.Equal(t, "my_report_v2", sanitizePDFStem("my report/v2"))
	require.Equal(t, "page", sanitizePDFStem(""))
}

func TestPDFRenderToolRejectsBadInputAndSaysWhenTheConverterIsMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4"), 0o644))
	tool := NewPDFRenderTool(dir)
	run := func(input string) fantasy.ToolResponse {
		resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "1", Name: PDFRenderToolName, Input: input})
		require.NoError(t, err)
		return resp
	}

	require.True(t, run(`{}`).IsError)
	require.True(t, run(`{"path":"a.pdf","page":0}`).IsError)
	require.True(t, run(`{"path":"a.pdf","dpi":9999}`).IsError)
	require.True(t, run(`{"path":"missing.pdf"}`).IsError)

	if _, err := exec.LookPath("pdftoppm"); err != nil {
		resp := run(`{"path":"a.pdf"}`)
		require.True(t, resp.IsError)
		require.Contains(t, resp.Content, "pdftoppm")
	}
}

// fakePdftoppmEnv makes this test binary behave like pdftoppm when it is run
// under that name: it writes "<prefix>.png" for the page it was asked for, the
// way Poppler does with -singlefile, and prints its arguments to stderr.
const fakePdftoppmEnv = "ATLAS_TEST_FAKE_PDFTOPPM"

func init() {
	if os.Getenv(fakePdftoppmEnv) != "1" || !strings.HasPrefix(filepath.Base(os.Args[0]), "pdftoppm") {
		return
	}
	args := os.Args[1:]
	if len(args) < 2 {
		os.Exit(2)
	}
	prefix := args[len(args)-1]
	for i, a := range args {
		if a == "-f" && i+1 < len(args) && args[i+1] == "99" {
			os.Stderr.WriteString("Wrong page range given: the first page (99) can not be after the last page (3).\n")
			os.Exit(99)
		}
	}
	if err := os.WriteFile(prefix+".png", []byte("PNG fake"), 0o644); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func installFakePdftoppm(t *testing.T) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	name := "pdftoppm"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := t.TempDir()
	src, err := os.Open(exe)
	require.NoError(t, err)
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(bin, name), os.O_CREATE|os.O_WRONLY, 0o755)
	require.NoError(t, err)
	_, err = io.Copy(dst, src)
	require.NoError(t, err)
	require.NoError(t, dst.Close())
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakePdftoppmEnv, "1")
}

// The tool's whole path with a converter on PATH: arguments, output location,
// the reported path, and a page past the end of the document.
func TestPDFRenderToolRunsTheConverterAndReportsTheImage(t *testing.T) {
	installFakePdftoppm(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my report.pdf"), []byte("%PDF-1.4"), 0o644))
	tool := NewPDFRenderTool(dir)
	run := func(input string) fantasy.ToolResponse {
		resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "1", Name: PDFRenderToolName, Input: input})
		require.NoError(t, err)
		return resp
	}

	ok := run(`{"path":"my report.pdf","page":2,"dpi":90}`)
	require.False(t, ok.IsError, ok.Content)
	require.Contains(t, ok.Content, ".atlas/pdf/my_report-p2-90dpi.png")
	_, err := os.Stat(filepath.Join(dir, ".atlas", "pdf", "my_report-p2-90dpi.png"))
	require.NoError(t, err)

	past := run(`{"path":"my report.pdf","page":99}`)
	require.True(t, past.IsError)
	require.Contains(t, past.Content, "Wrong page range")
}
