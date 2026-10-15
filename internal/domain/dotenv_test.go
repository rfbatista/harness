package domain

import (
	"slices"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		vars    []EnvVar
		skipped []int
	}{
		{"plain", "A=1\nB=two words\n", []EnvVar{{"A", "1"}, {"B", "two words"}}, nil},
		{"blank lines and comments", "\n# a comment\n  # indented\nA=1\n\n", []EnvVar{{"A", "1"}}, nil},
		{"crlf", "A=1\r\nB=2\r\n", []EnvVar{{"A", "1"}, {"B", "2"}}, nil},
		{"export prefix", "export A=1\nexport\tB=2\n", []EnvVar{{"A", "1"}, {"B", "2"}}, nil},
		{"spaces around", "  A = 1  \n", []EnvVar{{"A", "1"}}, nil},
		{"empty value", "A=\nB=''\nC=\"\"\n", []EnvVar{{"A", ""}, {"B", ""}, {"C", ""}}, nil},
		{"unquoted inline comment", "A=1 # one\nB=x#y\nC=#z\nD=  # nothing\n", []EnvVar{{"A", "1"}, {"B", "x#y"}, {"C", "#z"}, {"D", ""}}, nil},
		{"single quotes are literal", `A='a $B \n "q" #x' # c` + "\n", []EnvVar{{"A", `a $B \n "q" #x`}}, nil},
		{"double quote escapes", `A="l1\nl2 \"q\" back\\slash \t $B"` + "\n", []EnvVar{{"A", "l1\nl2 \"q\" back\\slash \\t $B"}}, nil},
		{"double quotes keep a #", `A="a # b" # c` + "\n", []EnvVar{{"A", "a # b"}}, nil},
		{"no substitution", "A=$(touch x)\nB=`id`\nC=${HOME}\n", []EnvVar{{"A", "$(touch x)"}, {"B", "`id`"}, {"C", "${HOME}"}}, nil},
		{"value with =", "URL=postgres://u:p@h/db?a=b\n", []EnvVar{{"URL", "postgres://u:p@h/db?a=b"}}, nil},
		{"later key wins in place", "A=1\nB=2\nA=3\n", []EnvVar{{"A", "3"}, {"B", "2"}}, nil},
		{
			"invalid lines are skipped by number",
			"A=1\nnot a pair\n1BAD=x\nB-C=x\nexport\n=x\nD='open\nE=\"open\nF='a'b\nG=ok\n",
			[]EnvVar{{"A", "1"}, {"G", "ok"}},
			[]int{2, 3, 4, 5, 6, 7, 8, 9},
		},
		{"nul byte is skipped", "A=a\x00b\nB=1\n", []EnvVar{{"B", "1"}}, []int{1}},
		{"empty text", "", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vars, skipped := ParseDotenv(c.text)
			if !slices.Equal(vars, c.vars) {
				t.Errorf("vars = %q, want %q", vars, c.vars)
			}
			if !slices.Equal(skipped, c.skipped) {
				t.Errorf("skipped = %v, want %v", skipped, c.skipped)
			}
		})
	}
}

func TestValidEnvKey(t *testing.T) {
	for _, k := range []string{"A", "_", "a_1", "API_TOKEN"} {
		if !ValidEnvKey(k) {
			t.Errorf("%q should be valid", k)
		}
	}
	for _, k := range []string{"", "1A", "A-B", "A B", "A.B", "É"} {
		if ValidEnvKey(k) {
			t.Errorf("%q should be invalid", k)
		}
	}
}
