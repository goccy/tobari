package overlay_test

import (
	"testing"

	"github.com/goccy/tobari/internal/overlay"
	"github.com/goccy/tobari/internal/utils"
)

func TestTargetPackages(t *testing.T) {
	targets := overlay.TargetPackages()
	expected := []string{"runtime", "testing", "testing/internal/testdeps"}
	for _, pkg := range expected {
		if _, ok := targets[pkg]; !ok {
			t.Errorf("expected package %s in TargetPackages", pkg)
		}
	}
	if len(targets) != len(expected) {
		t.Errorf("expected %d packages, got %d", len(expected), len(targets))
	}
}

func TestComputeHashDeterministic(t *testing.T) {
	h1, err := overlay.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	h2, err := overlay.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("expected same hash, got %s and %s", h1, h2)
	}
	if len(h1) != 16 {
		t.Fatalf("expected 16-char hash, got %d chars: %s", len(h1), h1)
	}
}

func TestRenderPackage(t *testing.T) {
	targets := overlay.TargetPackages()
	def := targets["runtime"]
	if def == nil {
		t.Fatal("runtime not in TargetPackages")
	}

	pkgPath, err := utils.GoPkgPath("runtime")
	if err != nil {
		t.Fatal(err)
	}
	files, err := utils.GoPkgFiles(pkgPath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := overlay.RenderPackage(def, files, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should have at least one replaced file (covercounter.go with coverage_getCovCounterList)
	if len(result.Replace) == 0 {
		t.Fatal("expected at least one replaced file")
	}

	// Should have exactly one added file (tobari.go)
	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added file, got %d", len(result.Added))
	}

	// Should have imports from runtime.go.tmpl
	if len(result.Imports) == 0 {
		t.Fatal("expected imports from tobari.go template")
	}
}
