package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestFormatDotenvReadsBackExactlyWhatWasSet(t *testing.T) {
	in := []EnvVar{{"PLAIN", "abc"}, {"EMPTY", ""}, {"SPACE", " a b "}, {"QUOTE", `say "hi" it's`}, {"NEWLINE", "a\nb"}, {"HASH", "a # b"}, {"SLASH", `a\b`}, {"URL", "http://h:1/p?q=1#frag"}}
	got, skipped := ParseDotenv(FormatDotenv(in))
	if len(skipped) != 0 || !reflect.DeepEqual(got, in) {
		t.Fatalf("read back %v (skipped %v), want %v", got, skipped, in)
	}
}

func TestApplyEnvChangesSetsReplacesAndUnsetsAndNamesTheKeyOfAProblem(t *testing.T) {
	text, changes, err := ApplyEnvChanges(ServiceKindCommand, "A=1\nB=2\nC=3\n", map[string]string{"B": "new", "D": "4", "A": "1"}, []string{"C", "NOPE"})
	if err != nil {
		t.Fatal(err)
	}
	if text != "A=1\nB=new\nD=4\n" {
		t.Fatalf("text = %q", text)
	}
	want := []ServiceRequestEnv{
		{Name: "B", Value: "new", Change: "set", PreviousSet: true},
		{Name: "D", Value: "4", Change: "set"},
		{Name: "C", Change: "unset", PreviousSet: true},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %v, want %v", changes, want)
	}
	if same, ch, _ := ApplyEnvChanges(ServiceKindCommand, "A=1 # kept\n", map[string]string{"A": "1"}, []string{"Z"}); same != "A=1 # kept\n" || ch != nil {
		t.Fatalf("a change of nothing keeps the text: %q %v", same, ch)
	}
	for _, c := range []struct {
		kind  ServiceKind
		set   map[string]string
		unset []string
		want  string
	}{
		{ServiceKindCommand, map[string]string{"1BAD": "x"}, nil, `env key "1BAD" is not a valid variable name`},
		{ServiceKindCommand, nil, []string{"A-B"}, `env key "A-B" is not a valid variable name`},
		{ServiceKindCommand, map[string]string{"X": "a\x00b"}, nil, `env value of "X" contains a NUL character`},
		{ServiceKindCommand, map[string]string{"X": "a\rb"}, nil, `env value of "X" contains a carriage return`},
		{ServiceKindImage, map[string]string{"X": "a\nb"}, nil, `env value of "X" contains a newline (image services)`},
	} {
		_, _, err := ApplyEnvChanges(c.kind, "", c.set, c.unset)
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "a\x00b") {
			t.Fatalf("err = %v, want %q", err, c.want)
		}
	}
	if _, _, err := ApplyEnvChanges(ServiceKindCommand, "", map[string]string{"X": "a\nb"}, nil); err != nil {
		t.Fatalf("a command service takes a newline: %v", err)
	}
}
