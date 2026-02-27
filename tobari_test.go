package tobari_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
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
		name     string
		flagsDir string   // directory for tobari flags (empty = project root)
		runDir   string   // directory for go run/test (empty = project root)
		runArgs  []string // extra args for go run after flags
		hasTest  bool     // supports go test
		cleanup  []string // files to remove after go run
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
			name:     "channel",
			flagsDir: "testdata/channel",
			runDir:   "testdata/channel",
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
		cmd := exec.CommandContext(ctx, "go", "test", ".", "-count=1")
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
				})
			}
		})
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
