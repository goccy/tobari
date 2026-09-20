package cover

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/packages"
)

// testImportGraph builds the package graph described by imports, which maps a
// package to the packages it imports, and returns the named roots.
func testImportGraph(imports map[string][]string, roots ...string) []*packages.Package {
	byID := make(map[string]*packages.Package)
	get := func(id string) *packages.Package {
		if byID[id] == nil {
			byID[id] = &packages.Package{ID: id, PkgPath: id, Imports: make(map[string]*packages.Package)}
		}
		return byID[id]
	}
	for id, imported := range imports {
		for _, dep := range imported {
			get(id).Imports[dep] = get(dep)
		}
	}
	pkgs := make([]*packages.Package, len(roots))
	for i, root := range roots {
		pkgs[i] = get(root)
	}
	return pkgs
}

// TestValidVerdicts checks which learned verdicts survive a change in how the
// code under development (app) uses its dependencies:
//
//	app -> b -> c -> shared
//	app -> d ------> shared
//	app -> e
func TestValidVerdicts(t *testing.T) {
	closures := newImportClosures(testImportGraph(map[string][]string{
		"app": {"b", "d", "e"},
		"b":   {"c"},
		"c":   {"shared"},
		"d":   {"shared"},
	}, "app"))

	record := &skipRecord{
		Boundaries: boundaries{"b": "b1", "d": "d1", "e": "e1"},
		Verdicts: map[string]SkipVerdict{
			"b":      VerdictSkip,
			"c":      VerdictSkip,
			"shared": VerdictSkip,
			"d":      VerdictKeep,
			"e":      VerdictSkip,
		},
	}

	tests := []struct {
		name    string
		current boundaries
		want    map[string]SkipVerdict
	}{
		{
			name:    "nothing changed: everything holds",
			current: boundaries{"b": "b1", "d": "d1", "e": "e1"},
			want:    record.Verdicts,
		},
		{
			name:    "the use of b changed: b and all it imports are analyzed again",
			current: boundaries{"b": "b2", "d": "d1", "e": "e1"},
			want:    map[string]SkipVerdict{"d": VerdictKeep, "e": VerdictSkip},
		},
		{
			name:    "c is now used directly: c and what it imports, but not b",
			current: boundaries{"b": "b1", "c": "c1", "d": "d1", "e": "e1"},
			want:    map[string]SkipVerdict{"b": VerdictSkip, "d": VerdictKeep, "e": VerdictSkip},
		},
		{
			name:    "e is no longer used: only e",
			current: boundaries{"b": "b1", "d": "d1"},
			want: map[string]SkipVerdict{
				"b":      VerdictSkip,
				"c":      VerdictSkip,
				"shared": VerdictSkip,
				"d":      VerdictKeep,
			},
		},
		{
			name:    "a keep verdict is dropped like any other",
			current: boundaries{"b": "b1", "d": "d2", "e": "e1"},
			want:    map[string]SkipVerdict{"b": VerdictSkip, "c": VerdictSkip, "e": VerdictSkip},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validVerdicts(record, test.current, closures)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("valid verdicts (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("no record: nothing is known", func(t *testing.T) {
		if got := validVerdicts(nil, boundaries{"b": "b1"}, closures); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
}

func TestImmutableVersion(t *testing.T) {
	tests := []struct {
		name    string
		pkg     listedPackage
		version string
		ok      bool
	}{
		{name: "standard library", pkg: listedPackage{Standard: true}, version: stdModule, ok: true},
		{name: "module with a version", pkg: listedPackage{Module: &listedModule{Path: "example.com/m", Version: "v1.2.3"}}, version: "example.com/m@v1.2.3", ok: true},
		{name: "main module", pkg: listedPackage{Module: &listedModule{Path: "example.com/app"}}},
		{name: "no module", pkg: listedPackage{}},
		{
			name: "replaced by a directory",
			pkg:  listedPackage{Module: &listedModule{Path: "example.com/m", Version: "v1.2.3", Replace: &listedModule{Path: "../m"}}},
		},
		{
			name:    "replaced by another version",
			pkg:     listedPackage{Module: &listedModule{Path: "example.com/m", Version: "v1.2.3", Replace: &listedModule{Path: "example.com/fork", Version: "v1.2.4"}}},
			version: "example.com/fork@v1.2.4",
			ok:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, ok := test.pkg.immutableVersion()
			if version != test.version || ok != test.ok {
				t.Errorf("got (%q, %v), want (%q, %v)", version, ok, test.version, test.ok)
			}
		})
	}
}

func TestAnalysisModeFromEnv(t *testing.T) {
	for value, want := range map[string]analysisMode{
		"":       analysisModeAuto,
		"full":   analysisModeFull,
		"verify": analysisModeVerify,
	} {
		t.Setenv(EnvAnalysis, value)
		got, err := analysisModeFromEnv()
		if err != nil || got != want {
			t.Errorf("%q: got (%q, %v), want %q", value, got, err, want)
		}
	}
	t.Setenv(EnvAnalysis, "off")
	if _, err := analysisModeFromEnv(); err == nil {
		t.Error("expected an unknown mode to be rejected")
	}
}
