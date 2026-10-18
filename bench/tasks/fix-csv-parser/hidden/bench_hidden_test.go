package csvline

import (
	"errors"
	"reflect"
	"testing"
)

func TestHiddenParse(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		{"plain", "a,b,c", []string{"a", "b", "c"}},
		{"empty line", "", []string{""}},
		{"single field", "solo", []string{"solo"}},
		{"trailing comma", "a,b,", []string{"a", "b", ""}},
		{"leading comma", ",a", []string{"", "a"}},
		{"only commas", ",,", []string{"", "", ""}},
		{"quoted comma", `"a,b",c`, []string{"a,b", "c"}},
		{"escaped quote", `"say ""hi""",x`, []string{`say "hi"`, "x"}},
		{"quote only field", `""""`, []string{`"`}},
		{"empty quoted", `"",a`, []string{"", "a"}},
		{"quoted last empty", `a,""`, []string{"a", ""}},
		{"spaces kept", " a , b ", []string{" a ", " b "}},
	}
	for _, tc := range cases {
		got, err := Parse(tc.line)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Parse(%q) = %q, want %q", tc.name, tc.line, got, tc.want)
		}
	}
}

func TestHiddenParseUnterminated(t *testing.T) {
	for _, line := range []string{`"abc`, `a,"b`, `"a""`} {
		if _, err := Parse(line); !errors.Is(err, ErrUnterminatedQuote) {
			t.Errorf("Parse(%q) error = %v, want ErrUnterminatedQuote", line, err)
		}
	}
}
