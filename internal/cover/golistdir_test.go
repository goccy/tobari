package cover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/tobari/internal/utils"
)

// writeCoverPkgCache seeds the global cover-package cache with one entry.
func writeCoverPkgCache(t *testing.T, pkgPath, dir string) {
	t.Helper()
	cacheDir := utils.CoverPkgsDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	data, err := json.Marshal(coverPkgCache{PkgPath: pkgPath, Dir: dir})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, coverPkgDirHash(dir)), data, 0o644); err != nil {
		t.Fatalf("write cache entry: %v", err)
	}
}

// TestResolveGoListDirModuleBoundary verifies that a module only claims cover
// targets from its own package path, not from a module whose path merely shares
// a string prefix. The cover-package cache is global, so entries from unrelated
// modules ("example.com/foobar") coexist with the module under analysis
// ("example.com/foo"). Matching without a path boundary used to pick a foreign
// directory and derive a go list directory containing no Go files.
func TestResolveGoListDirModuleBoundary(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	// A cover target belonging to the *unrelated* module example.com/foobar.
	foreignRoot := filepath.Join(tmp, "foobar")
	foreignPkg := filepath.Join(foreignRoot, "handler")
	if err := os.MkdirAll(foreignPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(foreignRoot, "go.mod"), []byte("module example.com/foobar\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	writeCoverPkgCache(t, "example.com/foobar/handler", foreignPkg)

	cfg := &PackageConfig{PkgPath: "example.com/foo.test", ModulePath: "example.com/foo"}
	dir, err := resolveGoListDir(nil, cfg)
	if err == nil {
		t.Fatalf("expected no directory for example.com/foo, got %q (leaked from example.com/foobar)", dir)
	}

	// Now add a cover target that genuinely belongs to example.com/foo.
	ownRoot := filepath.Join(tmp, "foo")
	ownPkg := filepath.Join(ownRoot, "store")
	if err := os.MkdirAll(ownPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownRoot, "go.mod"), []byte("module example.com/foo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	writeCoverPkgCache(t, "example.com/foo/store", ownPkg)

	dir, err = resolveGoListDir(nil, cfg)
	if err != nil {
		t.Fatalf("expected to resolve dir for example.com/foo: %v", err)
	}
	if dir != ownRoot {
		t.Errorf("resolveGoListDir = %q, want %q", dir, ownRoot)
	}
}
