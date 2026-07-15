package tobari_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/tobari"
	"github.com/google/go-cmp/cmp"
)

func TestTobari(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari-test")

	if out, err := exec.CommandContext(
		ctx,
		"go",
		"build",
		"-o",
		tobariBin,
		"./cmd/tobari",
	).CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}
	tobariFlagsOut, err := exec.CommandContext(ctx, tobariBin, "flags").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run tobari flags: %s: %v", string(tobariFlagsOut), err)
	}
	tobariFlags := strings.TrimSpace(string(tobariFlagsOut))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		dir        string
		input      string
		compareMap map[string]string
	}{
		{
			dir:   "http",
			input: "main.go",
			compareMap: map[string]string{
				"http.cover": "expected.cover",
			},
		},
		{
			dir:   "covername",
			input: "main.go",
			compareMap: map[string]string{
				"covername-foo1.cover": "covername-foo1.cover",
				"covername-foo2.cover": "covername-foo2.cover",
			},
		},
	} {
		t.Run(test.dir, func(t *testing.T) {
			if test.dir == "" {
				t.Fatal("dir is required")
			}
			if test.input == "" {
				t.Fatal("input name is required")
			}
			if len(test.compareMap) == 0 {
				t.Fatal("compareMap is required")
			}
			dir := filepath.Join("testdata", test.dir)

			args := append(
				[]string{"run"},
				strings.Split(tobariFlags, " ")...,
			)
			args = append(args, filepath.Join(dir, test.input))
			cmd := exec.CommandContext(
				ctx,
				"go",
				args...,
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("failed to run test: %s: %v", string(out), err)
			} else {
				t.Log(string(out))
			}
			for output, expected := range test.compareMap {
				t.Run(output, func(t *testing.T) {
					f, err := os.ReadFile(output)
					if err != nil {
						t.Fatal(err)
					}
					var out []string
					for _, line := range strings.Split(string(f), "\n") {
						if strings.HasPrefix(line, "/") {
							out = append(out, strings.TrimPrefix(line, cwd+"/"))
						} else {
							out = append(out, line)
						}
					}
					got := strings.Join(out, "\n")
					expected, err := os.ReadFile(filepath.Join(dir, expected))
					if err != nil {
						t.Fatal(err)
					}
					if diff := cmp.Diff(got, string(expected)); diff != "" {
						t.Errorf("(-got, +want)\n%s", diff)
					}
				})
			}
		})
	}
}

func TestZeroConfiguration(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari-test")
	defer func() {
		_ = os.RemoveAll(tobariBin)
	}()

	if out, err := exec.CommandContext(
		ctx,
		"go",
		"build",
		"-o",
		tobariBin,
		"./cmd/tobari",
	).CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}
	tobariFlagsOut, err := exec.CommandContext(ctx, tobariBin, "flags").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run tobari flags: %s: %v", string(tobariFlagsOut), err)
	}
	tobariFlags := strings.TrimSpace(string(tobariFlagsOut))
	args := append(append([]string{"test"}, strings.Split(tobariFlags, " ")...), ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = filepath.Join("testdata", "notobari")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to run test: %s: %v", string(out), err)
	} else {
		t.Log(string(out))
	}
}

func TestCacheBehavior(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari-test")

	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	type app struct {
		name         string
		flagsDir     string   // directory for tobari flags (empty = project root)
		runDir       string   // directory for go run/test (empty = project root)
		runArgs      []string // extra args for go run after flags
		testArgs     []string // extra args for go test
		hasTest      bool     // supports go test
		cleanup      []string // files to remove after go run
		expectedJSON string   // path to expected tobari.json (relative to runDir)
	}

	apps := []app{
		{
			name:    "http",
			runArgs: []string{"testdata/http/main.go"},
			cleanup: []string{"http.cover"},
		},
		{
			name:    "covername",
			runArgs: []string{"testdata/covername/main.go"},
			cleanup: []string{"covername-foo1.cover", "covername-foo2.cover"},
		},
		{
			name:     "notobari",
			flagsDir: "testdata/notobari",
			runDir:   "testdata/notobari",
			hasTest:  true,
		},
		{
			name:     "toolchain",
			flagsDir: "testdata/toolchain",
			runDir:   "testdata/toolchain",
			hasTest:  true,
		},
		{
			name:         "channel",
			flagsDir:     "testdata/channel",
			runDir:       "testdata/channel",
			testArgs:     []string{"-coverpkg=example.com/channel/..."},
			hasTest:      true,
			expectedJSON: "expected_tobari.json",
		},
		{
			name:     "synctest",
			flagsDir: "testdata/synctest",
			runDir:   "testdata/synctest",
			hasTest:  true,
		},
		{
			name:     "generic",
			flagsDir: "testdata/generic",
			runDir:   "testdata/generic",
			hasTest:  true,
		},
		{
			name:         "crosspkg",
			flagsDir:     "testdata/crosspkg",
			runDir:       "testdata/crosspkg",
			testArgs:     []string{"-coverpkg=example.com/..."},
			hasTest:      true,
			expectedJSON: "expected_tobari.json",
		},
		{
			name:         "thirdparty",
			flagsDir:     "testdata/thirdparty",
			runDir:       "testdata/thirdparty",
			testArgs:     []string{"-coverpkg=example.com/thirdparty/..."},
			hasTest:      true,
			expectedJSON: "expected_tobari.json",
		},
		{
			name:         "thirdparty_nonmain",
			flagsDir:     "testdata/thirdparty_nonmain",
			runDir:       "testdata/thirdparty_nonmain/lib",
			testArgs:     []string{"-coverpkg=example.com/thirdparty_nonmain/..."},
			hasTest:      true,
			expectedJSON: "expected_tobari.json",
		},
		{
			name:         "thirdparty_subdir",
			flagsDir:     "testdata/thirdparty_subdir",
			runDir:       "testdata/thirdparty_subdir/cmd/app",
			testArgs:     []string{"-coverpkg=example.com/thirdparty_subdir/..."},
			hasTest:      true,
			expectedJSON: "expected_tobari.json",
		},
		{
			name:     "initorder",
			flagsDir: "testdata/initorder",
			runDir:   "testdata/initorder",
			runArgs:  []string{"main.go"},
		},
		{
			name:     "initfunc",
			flagsDir: "testdata/initfunc",
			runDir:   "testdata/initfunc",
			runArgs:  []string{"main.go"},
			hasTest:  true,
		},
	}

	goFlagsEnv := func(tobariFlags []string) []string {
		return append(os.Environ(), "GOFLAGS="+strings.Join(tobariFlags, " "))
	}

	goRun := func(t *testing.T, a app, tobariFlags []string) {
		t.Helper()
		args := append([]string{"run"}, a.runArgs...)
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Env = goFlagsEnv(tobariFlags)
		if a.runDir != "" {
			cmd.Dir = a.runDir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go run failed: %s: %v", string(out), err)
		}
	}

	goTest := func(t *testing.T, a app, tobariFlags []string) {
		t.Helper()
		args := append([]string{"test", ".", "-count=1"}, a.testArgs...)
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Env = goFlagsEnv(tobariFlags)
		if a.runDir != "" {
			cmd.Dir = a.runDir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go test failed: %s: %v", string(out), err)
		}
	}

	for _, phase := range []string{"clean_cache", "with_cache"} {
		t.Run(phase, func(t *testing.T) {
			if phase == "clean_cache" {
				if out, err := exec.CommandContext(ctx, "go", "clean", "-cache").CombinedOutput(); err != nil {
					t.Fatalf("failed to clean cache: %s: %v", string(out), err)
				}
			}

			for _, a := range apps {
				t.Run(a.name, func(t *testing.T) {
					t.Cleanup(func() {
						for _, f := range a.cleanup {
							_ = os.Remove(f)
						}
					})

					// Get tobari flags
					flagsCmd := exec.CommandContext(ctx, tobariBin, "flags")
					if a.flagsDir != "" {
						flagsCmd.Dir = a.flagsDir
					}
					flagsOut, err := flagsCmd.CombinedOutput()
					if err != nil {
						t.Fatalf("tobari flags failed: %s: %v", string(flagsOut), err)
					}
					tobariFlags := strings.Split(strings.TrimSpace(string(flagsOut)), " ")

					if len(a.runArgs) > 0 {
						t.Run("go_run", func(t *testing.T) {
							goRun(t, a, tobariFlags)
						})
					}
					if a.hasTest {
						t.Run("go_test", func(t *testing.T) {
							goTest(t, a, tobariFlags)
						})
					}
					if a.expectedJSON != "" {
						t.Run("tobari_json", func(t *testing.T) {
							compareTobariJSON(t, a.runDir, a.expectedJSON)
						})
					}
				})
			}
		})
	}
}

// compareTobariJSON compares the generated tobari/tobari.json against an expected golden file.
// File paths in metadata.files are normalized to be relative to the test directory
// so the comparison is machine-independent.
func compareTobariJSON(t *testing.T, runDir, expectedFile string) {
	t.Helper()

	type tobariJSON struct {
		Metadata struct {
			Files []string `json:"files"`
			Entry []string `json:"entry"`
			All   [][]int  `json:"all"`
		} `json:"metadata"`
		Counts []struct {
			Name         string  `json:"name"`
			Coverprofile [][]int `json:"coverprofile"`
		} `json:"counts"`
		AllCounts []int `json:"allcounts"`
	}

	readJSON := func(path string) tobariJSON {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		var v tobariJSON
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}
		return v
	}

	actual := readJSON(filepath.Join(runDir, "tobari", "tobari.json"))
	expected := readJSON(filepath.Join(runDir, expectedFile))

	// Normalize absolute file paths to relative paths.
	absDir, err := filepath.Abs(runDir)
	if err != nil {
		t.Fatalf("failed to get abs path: %v", err)
	}
	for i, f := range actual.Metadata.Files {
		if rel, err := filepath.Rel(absDir, f); err == nil {
			actual.Metadata.Files[i] = rel
		}
	}

	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("tobari.json mismatch (-expected +actual):\n%s", diff)
	}
}

// TestFingerprintConsistency verifies that running go test with tobari multiple times
// produces consistent package fingerprints, allowing the Go build cache to work correctly.
//
// Background:
// Go computes package fingerprints (hashes) that include file paths of source files.
// When tobari modifies runtime/testing packages via overlay, the overlay file paths
// must be consistent across builds. If the paths change (e.g., include a build ID),
// the fingerprints will differ, causing "fingerprint mismatch" errors like:
//
//	fingerprint mismatch: runtime/coverage has X, import from testing expecting Y
//
// This test verifies that:
// 1. First go test with tobari succeeds (builds everything)
// 2. Second go test with tobari succeeds (uses cached packages with consistent fingerprints)
func TestFingerprintConsistency(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")

	// Build tobari
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	// Get tobari flags
	flagsCmd := exec.CommandContext(ctx, tobariBin, "flags")
	flagsCmd.Dir = "testdata/notobari"
	flagsOut, err := flagsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.Split(strings.TrimSpace(string(flagsOut)), " ")

	env := os.Environ()
	env = append(env, "GOFLAGS="+strings.Join(tobariFlags, " "))

	// Clean go build cache for a fresh start
	if out, err := exec.CommandContext(ctx, "go", "clean", "-cache").CombinedOutput(); err != nil {
		t.Fatalf("failed to clean cache: %s: %v", string(out), err)
	}

	// First run: builds everything including overlay-modified runtime/testing packages
	cmd1 := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
	cmd1.Env = env
	cmd1.Dir = "testdata/notobari"
	if out, err := cmd1.CombinedOutput(); err != nil {
		t.Fatalf("first go test failed: %s: %v", string(out), err)
	}

	// Second run: should use cached packages
	// Before the fix, this would fail with "fingerprint mismatch" because
	// the overlay directory path included BuildID which changed between runs,
	// causing different fingerprints for the same package content.
	cmd2 := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
	cmd2.Env = env
	cmd2.Dir = "testdata/notobari"
	out, err := cmd2.CombinedOutput()
	if err != nil {
		// Check if this is a fingerprint mismatch error
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch detected - overlay paths are not consistent: %s", string(out))
		}
		t.Fatalf("second go test failed: %s: %v", string(out), err)
	}

	// Third run: verify it continues to work
	cmd3 := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
	cmd3.Env = env
	cmd3.Dir = "testdata/notobari"
	if out, err := cmd3.CombinedOutput(); err != nil {
		t.Fatalf("third go test failed: %s: %v", string(out), err)
	}
}

// TestCrossPackageDeps verifies that go test -cover ./... (without -coverpkg)
// does not panic when suppDeps references functions from dependency packages
// that are not instrumented in the current test binary.
//
// Background:
// CreateMainDeps uses the global cover cache to build coverPkgSet, which
// may include dependency packages instrumented in other builds. The RTA
// analysis then generates suppDeps referencing those packages' functions.
// At runtime, funcMap only contains packages actually instrumented in this
// binary (via AddCoverMeta). resolveCandidateFuncMap must skip deps not
// in funcMap rather than panicking.
//
// Setup:
// testdata/crossdeps has pkga (depends on pkgb) and pkgb, both with tests.
// go test ./... tests both packages. When pkga's test runs, only pkga is
// instrumented, but pkgb may be in the global cover cache (from pkgb's test).
// suppDeps for pkga will reference pkgb.Double/pkgb.Greet, which are not
// in pkga's funcMap → must be skipped without panic.
func TestCrossPackageDeps(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")

	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	flagsCmd := exec.CommandContext(ctx, tobariBin, "flags")
	flagsCmd.Dir = "testdata/crossdeps"
	flagsOut, err := flagsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.Split(strings.TrimSpace(string(flagsOut)), " ")
	env := append(os.Environ(), "GOFLAGS="+strings.Join(tobariFlags, " "))

	if out, err := exec.CommandContext(ctx, "go", "clean", "-cache").CombinedOutput(); err != nil {
		t.Fatalf("failed to clean cache: %s: %v", string(out), err)
	}

	// Run go test ./... which tests both pkga and pkgb. Each test binary
	// only instruments its own package (-cover without -coverpkg), but the
	// global cover cache accumulates entries from all packages. pkga depends
	// on pkgb, so pkga's suppDeps will reference pkgb functions that are not
	// in pkga's funcMap.
	cmd := exec.CommandContext(ctx, "go", "test", "./...", "-count=1")
	cmd.Env = env
	cmd.Dir = "testdata/crossdeps"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test ./... failed: %s: %v", string(out), err)
	}
}

// TestTrimpath verifies that tobari works correctly with `-trimpath`.
//
// Background:
// When `go build -trimpath` or `go test -trimpath` is used (e.g., ko build),
// the inner build (GoListDepsExport) must also use `-trimpath` so that the
// inner and outer builds produce archives with matching fingerprints.
// tobari auto-detects -trimpath from compiler args (the value contains ';'
// when the user passes -trimpath) and propagates it to the inner build.
func TestTrimpath(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")

	// Build tobari
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	// Get tobari flags (trimpath is auto-detected, not a tobari flag)
	flagsCmd := exec.CommandContext(ctx, tobariBin, "flags")
	flagsCmd.Dir = "testdata/notobari"
	flagsOut, err := flagsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.TrimSpace(string(flagsOut))

	env := os.Environ()
	env = append(env, "GOFLAGS="+tobariFlags)

	// Run with -trimpath passed directly to go test (auto-detected by tobari)
	cmd1 := exec.CommandContext(ctx, "go", "test", "-trimpath", ".", "-count=1")
	cmd1.Env = env
	cmd1.Dir = "testdata/notobari"
	if out, err := cmd1.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch with -trimpath: %s", string(out))
		}
		t.Fatalf("first go test -trimpath failed: %s: %v", string(out), err)
	}

	// Second run: verify cache works with -trimpath
	cmd2 := exec.CommandContext(ctx, "go", "test", "-trimpath", ".", "-count=1")
	cmd2.Env = env
	cmd2.Dir = "testdata/notobari"
	if out, err := cmd2.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch on cached -trimpath build: %s", string(out))
		}
		t.Fatalf("second go test -trimpath failed: %s: %v", string(out), err)
	}
}

// TestRace verifies that tobari works correctly with `-race`.
//
// Background:
// When `go test -race` is used, the Go compiler receives `-race` and
// `-installsuffix race` flags, which change the build cache ID.
// tobari auto-detects -race from compiler args and propagates it to the
// inner build (GoListDepsExport) so that both builds produce packages
// with matching fingerprints.
func TestRace(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")

	// Build tobari
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	// Get tobari flags
	flagsCmd := exec.CommandContext(ctx, tobariBin, "flags")
	flagsCmd.Dir = "testdata/notobari"
	flagsOut, err := flagsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.TrimSpace(string(flagsOut))

	env := os.Environ()
	env = append(env, "GOFLAGS="+tobariFlags)

	// Clean go build cache for a fresh start
	if out, err := exec.CommandContext(ctx, "go", "clean", "-cache").CombinedOutput(); err != nil {
		t.Fatalf("failed to clean cache: %s: %v", string(out), err)
	}

	// First run: go test -race with tobari
	cmd1 := exec.CommandContext(ctx, "go", "test", "-race", ".", "-count=1")
	cmd1.Env = env
	cmd1.Dir = "testdata/notobari"
	if out, err := cmd1.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch with -race: %s", string(out))
		}
		t.Fatalf("first go test -race failed: %s: %v", string(out), err)
	}

	// Second run: verify cache works with -race
	cmd2 := exec.CommandContext(ctx, "go", "test", "-race", ".", "-count=1")
	cmd2.Env = env
	cmd2.Dir = "testdata/notobari"
	if out, err := cmd2.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch on cached -race build: %s", string(out))
		}
		t.Fatalf("second go test -race failed: %s: %v", string(out), err)
	}
}

// TestTags verifies that tobari works correctly with `-tags`.
//
// Background:
// When `go build -tags timetzdata` or `go test -tags timetzdata` is used,
// the Go toolchain compiles standard library packages (e.g., time) with
// different source files. Unlike -trimpath and -race, -tags is resolved by
// the `go` command before invoking the compiler, so tobari cannot auto-detect
// it from compiler arguments. Instead, the user specifies -tags via
// `tobari flags -tags=VALUE`, which outputs both -tags (for go build) and
// --build-tags (for tobari's toolexec) in GOFLAGS. tobari's inner builds
// inherit -tags from the filtered GOFLAGS, preventing fingerprint mismatches.
func TestTags(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")

	// Build tobari
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	// Get tobari flags with -tags=timetzdata.
	// This outputs: -cover -tags=timetzdata '-toolexec=tobari --build-tags=timetzdata'
	flagsCmd := exec.CommandContext(ctx, tobariBin, "flags", "-tags=timetzdata")
	flagsCmd.Dir = "testdata/notobari"
	flagsOut, err := flagsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.TrimSpace(string(flagsOut))

	env := os.Environ()
	env = append(env, "GOFLAGS="+tobariFlags)

	// Clean go build cache for a fresh start
	if out, err := exec.CommandContext(ctx, "go", "clean", "-cache").CombinedOutput(); err != nil {
		t.Fatalf("failed to clean cache: %s: %v", string(out), err)
	}

	// First run: go test with -tags timetzdata via tobari
	cmd1 := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
	cmd1.Env = env
	cmd1.Dir = "testdata/notobari"
	if out, err := cmd1.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch with -tags timetzdata: %s", string(out))
		}
		t.Fatalf("first go test -tags timetzdata failed: %s: %v", string(out), err)
	}

	// Second run: verify cache works with -tags
	cmd2 := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
	cmd2.Env = env
	cmd2.Dir = "testdata/notobari"
	if out, err := cmd2.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch on cached -tags timetzdata build: %s", string(out))
		}
		t.Fatalf("second go test -tags timetzdata failed: %s: %v", string(out), err)
	}
}

// TestFingerprintMismatchVersionDifference verifies that tobari works correctly
// when the tobari binary version differs from the version used by the target module.
//
// Background:
// When a user installs tobari at version X but their application depends on
// tobari at version Y (via go.mod), the inner build (buildPackages) must use
// version Y — not X — to compile the tobari library packages. Otherwise, the
// overlay-modified runtime packages reference tobari packages at version X while
// the target links against version Y, causing fingerprint mismatches at link time.
//
// This test builds the tobari binary with a fake version (v99.99.99) via ldflags
// and runs it against a module that depends on tobari via a local replace directive.
// Without the fix, the inner build would try to fetch v99.99.99 from the module proxy
// and fail. With the fix, it detects the target module's replace directive and uses
// the local path instead.
func TestFingerprintMismatchVersionDifference(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")

	// Build tobari with a fake version that doesn't exist on the module proxy.
	if out, err := exec.CommandContext(ctx, "go", "build",
		"-ldflags", "-X github.com/goccy/tobari/internal/version.ver=v99.99.99",
		"-o", tobariBin,
		"./cmd/tobari",
	).CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	// Get tobari flags for a module that depends on tobari (via replace directive).
	flagsCmd := exec.CommandContext(ctx, tobariBin, "flags")
	flagsCmd.Dir = "testdata/crosspkg"
	flagsOut, err := flagsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.Split(strings.TrimSpace(string(flagsOut)), " ")

	env := os.Environ()
	env = append(env, "GOFLAGS="+strings.Join(tobariFlags, " "))

	// Clean go build cache
	if out, err := exec.CommandContext(ctx, "go", "clean", "-cache").CombinedOutput(); err != nil {
		t.Fatalf("failed to clean cache: %s: %v", string(out), err)
	}

	// Run go test against a module that depends on tobari.
	// Without the fix, this fails because the inner build tries to fetch v99.99.99.
	// With the fix, it detects the target module's replace directive and succeeds.
	cmd := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
	cmd.Env = env
	cmd.Dir = "testdata/crosspkg"
	if out, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch with version difference: %s", string(out))
		}
		t.Fatalf("go test failed: %s: %v", string(out), err)
	}

	// Second run: verify cache works with version mismatch.
	cmd2 := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
	cmd2.Env = env
	cmd2.Dir = "testdata/crosspkg"
	if out, err := cmd2.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "fingerprint mismatch") {
			t.Fatalf("fingerprint mismatch on cached build with version difference: %s", string(out))
		}
		t.Fatalf("second go test failed: %s: %v", string(out), err)
	}
}

func TestEmbedCode(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()
	tobariBin := filepath.Join(tmpDir, "tobari-test")

	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	// Get tobari flags with -E (embed-code)
	flagsOut, err := exec.CommandContext(ctx, tobariBin, "flags", "-E").CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags -E failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.TrimSpace(string(flagsOut))

	// Build the embedcode test program
	testBin := filepath.Join(tmpDir, "embedcode-test")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", testBin, "testdata/embedcode/main.go")
	buildCmd.Env = append(os.Environ(), "GOFLAGS="+tobariFlags)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build with -E failed: %s: %v", string(out), err)
	}

	// Run the built binary
	out, err := exec.CommandContext(ctx, testBin).CombinedOutput()
	if err != nil {
		t.Fatalf("embedcode binary failed: %s: %v", string(out), err)
	}

	output := strings.TrimSpace(string(out))
	if output == "NO_SOURCES" {
		t.Fatal("ReadCoverArchivedFile returned nil; expected embedded sources")
	}

	// Build expected "name\thash" lines from actual source files
	goFiles, err := filepath.Glob("testdata/embedcode/*.go")
	if err != nil {
		t.Fatalf("failed to glob testdata/embedcode: %v", err)
	}
	var expected []string
	for _, f := range goFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read %s: %v", f, err)
		}
		absPath, err := filepath.Abs(f)
		if err != nil {
			t.Fatalf("failed to get abs path for %s: %v", f, err)
		}
		h := sha256.Sum256(data)
		expected = append(expected, fmt.Sprintf("%s\t%x", filepath.ToSlash(absPath), h))
	}
	sort.Strings(expected)

	actual := strings.Split(output, "\n")
	sort.Strings(actual)

	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Fatalf("embedded files mismatch (-expected +actual):\n%s", diff)
	}
}

func TestExtract(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()
	tobariBin := filepath.Join(tmpDir, "tobari-test")

	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	// Get tobari flags with -E (embed-code)
	flagsOut, err := exec.CommandContext(ctx, tobariBin, "flags", "-E").CombinedOutput()
	if err != nil {
		t.Fatalf("tobari flags -E failed: %s: %v", string(flagsOut), err)
	}
	tobariFlags := strings.TrimSpace(string(flagsOut))

	// Build the embedcode test program with -E
	testBin := filepath.Join(tmpDir, "embedcode-test")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", testBin, "testdata/embedcode/main.go")
	buildCmd.Env = append(os.Environ(), "GOFLAGS="+tobariFlags)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build with -E failed: %s: %v", string(out), err)
	}

	// Extract embedded sources using tobari extract
	outputFile := filepath.Join(tmpDir, "sources.tar.gz")
	extractCmd := exec.CommandContext(ctx, tobariBin, "extract", "-o", outputFile, testBin)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		t.Fatalf("tobari extract failed: %s: %v", string(out), err)
	}

	// Verify the output file exists
	info, err := os.Stat(outputFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}

	// Extract tar.gz and verify contents match original sources
	actual := extractAndHashTarGz(t, outputFile)

	goFiles, err := filepath.Glob("testdata/embedcode/*.go")
	if err != nil {
		t.Fatalf("failed to glob testdata/embedcode: %v", err)
	}
	var expected []string
	for _, f := range goFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read %s: %v", f, err)
		}
		absPath, err := filepath.Abs(f)
		if err != nil {
			t.Fatalf("failed to get abs path for %s: %v", f, err)
		}
		h := sha256.Sum256(data)
		expected = append(expected, fmt.Sprintf("%s\t%x", filepath.ToSlash(absPath), h))
	}
	sort.Strings(expected)

	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Fatalf("extracted files mismatch (-expected +actual):\n%s", diff)
	}
}

func extractAndHashTarGz(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open %s: %v", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer func() {
		_ = gr.Close()
	}()

	type entry struct {
		name string
		hash string
	}
	var entries []entry
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		h := sha256.Sum256(data)
		entries = append(entries, entry{
			name: hdr.Name,
			hash: fmt.Sprintf("%x", h),
		})
	}

	var result []string
	for _, e := range entries {
		result = append(result, fmt.Sprintf("%s\t%s", e.name, e.hash))
	}
	sort.Strings(result)
	return result
}

func TestMergeCoverArchivedFiles_DeterministicOutput(t *testing.T) {
	inputA := createTestTarGzData(t, map[string]string{
		"/src/main.go":  "package main\n",
		"/src/util.go":  "package main\n\nfunc util() {}\n",
		"/README.md":    "example\n",
		"/LICENSE.txt":  "license\n",
		"/docs/doc.txt": "doc\n",
	})
	inputB := createTestTarGzData(t, map[string]string{
		"/src/extra.go": "package main\n\nfunc extra() {}\n",
	})

	var out1 bytes.Buffer
	if err := tobari.MergeCoverArchivedFiles([]io.Reader{
		bytes.NewReader(inputA),
		bytes.NewReader(inputB),
	}, &out1); err != nil {
		t.Fatalf("MergeCoverArchivedFiles first run failed: %v", err)
	}

	var out2 bytes.Buffer
	if err := tobari.MergeCoverArchivedFiles([]io.Reader{
		bytes.NewReader(inputA),
		bytes.NewReader(inputB),
	}, &out2); err != nil {
		t.Fatalf("MergeCoverArchivedFiles second run failed: %v", err)
	}

	if !bytes.Equal(out1.Bytes(), out2.Bytes()) {
		t.Fatal("merged tar.gz is not deterministic across runs")
	}
}

func createTestTarGzData(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		content := []byte(files[name])
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header for %s: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write content for %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

// TestSuppDepsDeterministic verifies that repeated builds of the same program
// produce identical supplementary dependency data.
//
// The analysis derives its roots from maps (ssa.Program.AllPackages and
// ssa.Package.Members), and rta.Analyze materializes a call-graph node for
// roots[0] even when that root has no call edges. An unstable root order
// therefore used to add or drop entries from suppDeps between builds, silently
// changing coverage denominators.
func TestSuppDepsDeterministic(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")

	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	const runs = 5
	var first string
	for i := range runs {
		got := buildCrosspkgSuppDeps(t, ctx, tobariBin, "")
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("suppDeps differ between builds\nrun 1: %s\nrun %d: %s", first, i+1, got)
		}
	}
}

// TestExcludeAnalysisIgnoresCoverTargets verifies that naming a coverage-target
// package in --exclude-analysis is ignored: its SSA is still built and its
// dependency edges are preserved. Excluding a genuinely irrelevant package, by
// contrast, must not affect a coverage target's edges either. In both cases the
// suppDeps must equal the baseline (no exclusion) for this program, whose cover
// targets only pass plain data across package boundaries.
func TestExcludeAnalysisIgnoresCoverTargets(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	baseline := buildCrosspkgSuppDeps(t, ctx, tobariBin, "")

	tests := []struct {
		name    string
		exclude string
	}{
		{"exclude a coverage-target package (must be ignored)", "example.com/crosspkg/transform"},
		{"exclude all coverage-target packages (all ignored)", "example.com/crosspkg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCrosspkgSuppDeps(t, ctx, tobariBin, tt.exclude)
			if got != baseline {
				t.Errorf("excluding %q changed suppDeps (coverage target should be ignored)\nbaseline: %s\ngot:      %s",
					tt.exclude, baseline, got)
			}
		})
	}
}

// buildCrosspkgSuppDeps builds testdata/crosspkg with tobari and returns the
// suppDeps JSON embedded in the resulting binary. A fresh Go build cache forces
// the cover tool (and thus the analysis) to run again; a fresh cover-package
// cache keeps the discovered coverage targets identical across builds. When
// excludeAnalysis is non-empty it is passed via --exclude-analysis.
func buildCrosspkgSuppDeps(t *testing.T, ctx context.Context, tobariBin, excludeAnalysis string) string {
	t.Helper()
	if err := os.RemoveAll(coverPkgsDir()); err != nil {
		t.Fatalf("failed to clear cover pkg cache: %v", err)
	}
	toolexec := tobariBin
	if excludeAnalysis != "" {
		toolexec += " --exclude-analysis=" + excludeAnalysis
	}
	bin := filepath.Join(t.TempDir(), "app")
	cmd := exec.CommandContext(ctx, "go", "build",
		"-cover", "-toolexec="+toolexec, "-o", bin, ".")
	cmd.Dir = "testdata/crosspkg"
	cmd.Env = append(os.Environ(), "GOCACHE="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %s: %v", string(out), err)
	}
	return extractSuppDeps(t, bin)
}

// coverPkgsDir mirrors utils.CoverPkgsDir, which is in an internal package.
func coverPkgsDir() string {
	return filepath.Join(os.TempDir(), "tobari", "coverpkgs")
}

// extractSuppDeps returns the suppDeps JSON object embedded in a tobari-built
// binary. The cover tool serializes the map and the compile tool bakes it in as
// a string literal, so it appears verbatim in the binary's data section.
func extractSuppDeps(t *testing.T, binPath string) string {
	t.Helper()
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("failed to read binary: %v", err)
	}
	// Locate the JSON object whose first key is a crosspkg function.
	marker := []byte(`{"`)
	for i := 0; i+len(marker) < len(data); i++ {
		if !bytes.HasPrefix(data[i:], marker) {
			continue
		}
		end := bytes.IndexByte(data[i:], '}')
		if end < 0 {
			continue
		}
		candidate := data[i : i+end+1]
		if !bytes.Contains(candidate, []byte("example.com/crosspkg")) {
			continue
		}
		var m map[string][]string
		if err := json.Unmarshal(candidate, &m); err != nil {
			continue // a nested object; keep scanning
		}
		return string(candidate)
	}
	t.Fatal("suppDeps not found in binary")
	return ""
}
