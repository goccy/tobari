package utils

import (
	"reflect"
	"testing"
)

func TestParsePkgPrefixes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "github.com/org/repo", []string{"github.com/org/repo"}},
		{
			"multiple",
			"github.com/org/repo/internal/foo,github.com/org/other/pb",
			[]string{"github.com/org/repo/internal/foo", "github.com/org/other/pb"},
		},
		{
			"trims spaces and drops empties",
			" github.com/org/a , ,github.com/org/b ,",
			[]string{"github.com/org/a", "github.com/org/b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParsePkgPrefixes(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePkgPrefixes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMatchesPkgPrefix(t *testing.T) {
	// A sub-package prefix must exclude only that package and its descendants,
	// never a sibling, a parent, or a package that merely shares a string prefix.
	prefixes := ParsePkgPrefixes("github.com/org/foo,github.com/org/repo/internal/foo,github.com/org/other/pb")

	tests := []struct {
		pkg  string
		want bool
	}{
		{"github.com/org/foo", true},       // exact match
		{"github.com/org/foo/sub", true},   // descendant
		{"github.com/org/foobar", false},   // string-prefix trap: foo vs foobar
		{"github.com/org/foobar/x", false}, // descendant of the trap sibling

		{"github.com/org/repo/internal/foo", true},      // exact match
		{"github.com/org/repo/internal/foo/deep", true}, // descendant
		{"github.com/org/other/pb", true},               // second prefix
		{"github.com/org/repo/internal/bar", false},     // sibling
		{"github.com/org/repo/internal", false},         // parent
		{"github.com/org/repo", false},                  // module root
		{"github.com/org/other/pbx", false},             // string-prefix trap
		{"github.com/org/otherpb", false},               // string-prefix trap
		{"", false},
	}
	for _, tt := range tests {
		if got := MatchesPkgPrefix(tt.pkg, prefixes); got != tt.want {
			t.Errorf("MatchesPkgPrefix(%q) = %v, want %v", tt.pkg, got, tt.want)
		}
	}
}

func TestMatchesPkgPrefixNoPrefixes(t *testing.T) {
	if MatchesPkgPrefix("github.com/org/repo", nil) {
		t.Error("no prefixes must never match")
	}
}
