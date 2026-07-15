package cover

import "testing"

func TestIsExcludedFromAnalysis(t *testing.T) {
	coverPkgSet := map[string]struct{}{
		"example.com/app/handler": {},
		"example.com/app/store":   {},
	}
	prefixes := []string{
		"example.com/app/store",    // a coverage target: must be ignored
		"example.com/app/handler",  // a coverage target: must be ignored
		"github.com/org/generated", // irrelevant third party: excluded
		"example.com/app/internal/x",
	}

	tests := []struct {
		name string
		pkg  string
		want bool
	}{
		{"third party matching prefix is excluded", "github.com/org/generated", true},
		{"subpackage of excluded third party", "github.com/org/generated/pb/v1", true},
		{"non-cover internal package matching prefix", "example.com/app/internal/x", true},

		{"coverage target is never excluded", "example.com/app/store", false},
		{"coverage target (handler) is never excluded", "example.com/app/handler", false},
		{"test variant of a coverage target is never excluded", "example.com/app/store [example.com/app.test]", false},

		{"main is never excluded", "main", false},
		{"test binary is never excluded", "example.com/app.test", false},

		{"package not matching any prefix", "example.com/app/other", false},
		{"string-prefix trap", "github.com/org/generatedx", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExcludedFromAnalysis(tt.pkg, prefixes, coverPkgSet); got != tt.want {
				t.Errorf("isExcludedFromAnalysis(%q) = %v, want %v", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestIsExcludedFromAnalysisNoPrefixes(t *testing.T) {
	if isExcludedFromAnalysis("github.com/org/generated", nil, nil) {
		t.Error("no prefixes must never exclude")
	}
}

func TestIsMainOrTestPkg(t *testing.T) {
	tests := []struct {
		pkg  string
		want bool
	}{
		{"main", true},
		{"example.com/app.test", true},
		{"example.com/app/store [example.com/app.test]", true},
		{"example.com/app/store", false},
		{"maintenance", false},
	}
	for _, tt := range tests {
		if got := isMainOrTestPkg(tt.pkg); got != tt.want {
			t.Errorf("isMainOrTestPkg(%q) = %v, want %v", tt.pkg, got, tt.want)
		}
	}
}
