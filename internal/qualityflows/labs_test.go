package qualityflows

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func datasetFixture() DatasetSpec {
	return DatasetSpec{Version: 1, Seed: 42, Output: "fixtures.json", Entities: []DatasetEntity{
		{Name: "users", Count: 4, Fields: []DatasetField{{Name: "id", Kind: "id", Prefix: "u_"}, {Name: "email", Kind: "email"}, {Name: "name", Kind: "string", EmptyEvery: 2}, {Name: "created", Kind: "date", Start: "2026-01-01T00:00:00Z", StepSeconds: 86400}}},
		{Name: "orders", Count: 8, Fields: []DatasetField{{Name: "id", Kind: "id", Prefix: "o_"}, {Name: "user_id", Kind: "reference", Entity: "users", Field: "id"}, {Name: "amount", Kind: "int", Min: 0, Max: 100}, {Name: "status", Kind: "enum", Values: []string{"new", "paid"}, NullEvery: 3}, {Name: "flag", Kind: "bool"}}},
	}}
}

func TestGenerateDatasetReproducesRelatedFixtures(t *testing.T) {
	t.Parallel()
	spec := datasetFixture()
	first, err := GenerateDataset(t.Context(), spec)
	require.NoError(t, err)
	second, err := GenerateDataset(t.Context(), spec)
	require.NoError(t, err)
	require.Equal(t, first, second)
	spec.Seed++
	changed, err := GenerateDataset(t.Context(), spec)
	require.NoError(t, err)
	require.NotEqual(t, first, changed)
	var data map[string][]map[string]any
	require.NoError(t, json.Unmarshal(first, &data))
	parents := map[any]bool{}
	for _, row := range data["users"] {
		parents[row["id"]] = true
		require.Contains(t, row["email"], "@example.invalid")
	}
	for _, row := range data["orders"] {
		require.True(t, parents[row["user_id"]])
		require.GreaterOrEqual(t, row["amount"].(float64), 0.0)
		require.LessOrEqual(t, row["amount"].(float64), 100.0)
	}
	require.Empty(t, data["users"][1]["name"])
	require.Nil(t, data["orders"][2]["status"])
	require.Equal(t, "2026-01-04T00:00:00Z", data["users"][3]["created"])
}

func TestGenerateDatasetRejectsInvalidPlansBeforeOutput(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*DatasetSpec){
		func(s *DatasetSpec) { s.Entities[0].Fields[0].NullEvery = 1 },
		func(s *DatasetSpec) { s.Entities[1].Fields[1].Entity = "missing" },
		func(s *DatasetSpec) {
			s.Entities[0].Fields[3].Start = "2099-12-31T00:00:00Z"
			s.Entities[0].Fields[3].StepSeconds = 86400 * 366
		},
		func(s *DatasetSpec) { s.Output = "../escape.json" },
		func(s *DatasetSpec) { s.Entities[1].Fields[2].Max = -1 },
	} {
		spec := datasetFixture()
		mutate(&spec)
		_, err := GenerateDataset(t.Context(), spec)
		require.Error(t, err)
	}
}

func TestAssumptionsMeasureAllRulesAndExactDecimals(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "data.json"), []byte(`{"users":[{"id":1},{"id":2}],"orders":[{"id":"a","user":1,"amount":0.1},{"id":"b","user":2,"amount":0.2}],"a/b":{"~x":null}}`), 0o600))
	zero, two := int64(0), int64(2)
	spec := AssumptionSpec{Version: 1, Checks: []Assumption{
		{Name: "exists null", File: "data.json", Pointer: "/a~1b/~0x", Rule: "exists"},
		{Name: "nonempty", File: "data.json", Pointer: "/orders", Rule: "nonempty"},
		{Name: "equals numeric spelling", File: "data.json", Pointer: "/users/0/id", Rule: "equals", Expected: json.RawMessage(`1.0`)},
		{Name: "count", File: "data.json", Pointer: "/orders", Rule: "count", Min: &two, Max: &two},
		{Name: "required", File: "data.json", Pointer: "/orders", Rule: "required", Field: "/id"},
		{Name: "unique", File: "data.json", Pointer: "/orders", Rule: "unique", Field: "/id"},
		{Name: "range", File: "data.json", Pointer: "/orders", Rule: "range", Field: "/amount", Min: &zero, Max: &two},
		{Name: "ledger", File: "data.json", Pointer: "/orders", Rule: "sum_equals", Field: "/amount", Expected: json.RawMessage(`0.3`)},
		{Name: "parent", File: "data.json", Pointer: "/orders", Rule: "references", Field: "/user", ReferenceFile: "data.json", ReferencePointer: "/users", ReferenceField: "/id"},
	}}
	report, err := RunAssumptions(t.Context(), root, spec)
	require.NoError(t, err)
	require.True(t, report.Passed, report)
	require.Len(t, report.InputHashes, 1)
	for _, result := range report.Results {
		require.True(t, result.Observed)
	}
}

func TestAssumptionsFailOnCounterexamplesMissingDataAndHugeNumbers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "bad.json"), []byte(`[{"id":1},{"id":1.0}]`), 0o600))
	spec := AssumptionSpec{Version: 1, Checks: []Assumption{{Name: "duplicates", File: "bad.json", Rule: "unique", Field: "/id"}, {Name: "missing", File: "missing.json", Rule: "exists"}}}
	report, err := RunAssumptions(t.Context(), root, spec)
	require.NoError(t, err)
	require.False(t, report.Passed)
	require.Equal(t, 1, *report.Results[0].Counterexample)
	require.False(t, report.Results[1].Observed)
	_, err = jsonValue([]byte(`1e999999999`))
	require.Error(t, err)
	_, err = ResolvePointer([]any{1}, "/01")
	require.Error(t, err)
	_, err = ResolvePointer(map[string]any{}, "/~2")
	require.Error(t, err)
}

func TestBoundaryDetectsEncodedLeaksAndPositiveControlWithoutSnippets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	probe, err := PrepareBoundary(root, BoundarySpec{Version: 1, Action: "prepare", Labels: []string{"customer"}})
	require.NoError(t, err)
	marker := probe.Markers["customer"]
	require.NoError(t, os.WriteFile(filepath.Join(root, "control.txt"), []byte(marker), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "logs"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "logs", "out.txt"), []byte("unrelated secret must not appear in report\n"+base64.StdEncoding.EncodeToString([]byte(marker))), 0o600))
	utf16 := []byte{0xff, 0xfe}
	for i := range len(marker) {
		utf16 = append(utf16, marker[i], 0)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "logs", "wide.txt"), utf16, 0o600))
	spec := BoundarySpec{Version: 1, Action: "scan", ProbeID: probe.ID, Targets: []BoundaryTarget{{Path: "control.txt", Expect: "present"}, {Path: "logs", Expect: "absent"}}}
	report, err := ScanBoundary(t.Context(), root, spec, probe)
	require.NoError(t, err)
	require.False(t, report.Passed)
	require.True(t, report.Results[0].Passed)
	require.Equal(t, 2, report.Results[1].Counts["customer"])
	require.Len(t, report.InputHashes, 3)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "unrelated secret")
	require.NotContains(t, string(encoded), marker)
	require.Equal(t, "base64", report.Matches[1].Encoding)
	require.Equal(t, "utf16le", report.Matches[2].Encoding)
	require.NoError(t, os.WriteFile(filepath.Join(root, "logs", "out.txt"), []byte("clean"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "logs", "wide.txt"), []byte("clean"), 0o600))
	report, err = ScanBoundary(t.Context(), root, spec, probe)
	require.NoError(t, err)
	require.True(t, report.Passed)
}

func TestBoundaryCannotPassMissingEmptyOrForeignInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	probe, err := PrepareBoundary(root, BoundarySpec{Version: 1, Action: "prepare", Labels: []string{"test"}})
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0o700))
	spec := BoundarySpec{Version: 1, Action: "scan", ProbeID: probe.ID, Targets: []BoundaryTarget{{Path: "missing.txt", Expect: "absent"}, {Path: "empty", Expect: "absent"}}}
	report, err := ScanBoundary(t.Context(), root, spec, probe)
	require.NoError(t, err)
	require.False(t, report.Passed)
	_, err = ScanBoundary(t.Context(), t.TempDir(), spec, probe)
	require.Error(t, err)
	spec.Targets[0].Labels = []string{"unknown"}
	_, err = ScanBoundary(t.Context(), root, spec, probe)
	require.Error(t, err)
}

func TestBoundaryRequiresPositiveControlForAbsence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	probe, err := PrepareBoundary(root, BoundarySpec{Version: 1, Action: "prepare", Labels: []string{"customer"}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "clean.txt"), []byte("public output"), 0o600))
	spec := BoundarySpec{Version: 1, Action: "scan", ProbeID: probe.ID, Targets: []BoundaryTarget{{Path: "clean.txt", Expect: "absent"}}}
	report, err := ScanBoundary(t.Context(), root, spec, probe)
	require.NoError(t, err)
	require.False(t, report.Passed)
	require.Contains(t, report.Results[0].Reason, "positive control")
}
