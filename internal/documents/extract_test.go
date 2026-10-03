package documents

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func officeFixture(t *testing.T, ext string, parts map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture"+ext)
	f, err := os.Create(path)
	require.NoError(t, err)
	z := zip.NewWriter(f)
	for name, data := range parts {
		w, err := z.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(data))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	require.NoError(t, f.Close())
	return path
}

func TestDocumentSourcePositions(t *testing.T) {
	t.Parallel()
	docx := officeFixture(t, ".docx", map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t> world</w:t></w:r></w:p><w:p><w:r><w:t>Next</w:t></w:r></w:p></w:body></w:document>`})
	r, err := Extract(t.Context(), docx)
	require.NoError(t, err)
	require.Len(t, r.Sections, 2)
	require.Equal(t, "paragraph:1", r.Sections[0].Source)
	require.Equal(t, "Hello world", r.Sections[0].Text)
	xlsx := officeFixture(t, ".xlsx", map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="Budget" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>Revenue</t></si></sst>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c r="A1" t="s"><v>0</v></c><c r="B1"><f>SUM(B2:B3)</f><v>42</v></c></row></sheetData></worksheet>`,
	})
	r, err = Extract(t.Context(), xlsx)
	require.NoError(t, err)
	require.Equal(t, "sheet:Budget/cell:A1", r.Sections[0].Source)
	require.Equal(t, "Revenue", r.Sections[0].Text)
	require.Contains(t, r.Sections[1].Text, "not recalculated")
	pptx := officeFixture(t, ".pptx", map[string]string{
		"ppt/slides/slide10.xml": `<slide><p><t>Later</t></p></slide>`,
		"ppt/slides/slide2.xml":  `<slide><p><t>Earlier</t></p></slide>`,
	})
	r, err = Extract(t.Context(), pptx)
	require.NoError(t, err)
	require.Equal(t, "slide:2/paragraph:1", r.Sections[0].Source)
	require.Equal(t, "Earlier", r.Sections[0].Text)
}

func TestDocumentOutputIsBoundedBeforeAccumulation(t *testing.T) {
	t.Parallel()
	r := Result{}
	for range 100 {
		r.add(Section{Source: "paragraph:1", Text: strings.Repeat("ş", 32768)})
	}
	require.True(t, r.Truncated)
	require.LessOrEqual(t, r.extracted, maxExtractedBytes)
	for _, section := range r.Sections {
		require.True(t, utf8.ValidString(section.Text))
	}
}

func TestNotebookDoesNotExecuteAndReportsCell(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.ipynb")
	require.NoError(t, os.WriteFile(path, []byte(`{"nbformat":4,"cells":[{"cell_type":"code","source":["print('hello')"],"outputs":[{"text":["hello\n"]}]}]}`), 0o600))
	r, err := Extract(t.Context(), path)
	require.NoError(t, err)
	require.Equal(t, "cell:1/code", r.Sections[0].Source)
	require.Equal(t, "cell:1/output:1", r.Sections[1].Source)
	require.NotEmpty(t, r.Warnings)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Extract(ctx, path)
	require.Error(t, err)
}

func TestMalformedOfficeAndExternalSheetAreRefused(t *testing.T) {
	t.Parallel()
	path := officeFixture(t, ".docx", map[string]string{"word/document.xml": "<broken"})
	_, err := Extract(t.Context(), path)
	require.Error(t, err)
	path = officeFixture(t, ".xlsx", map[string]string{"xl/workbook.xml": `<workbook xmlns:r="r"><sheets><sheet name="A" r:id="id"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="id" Target="../../outside.xml"/></Relationships>`})
	_, err = Extract(t.Context(), path)
	require.Error(t, err)
}
