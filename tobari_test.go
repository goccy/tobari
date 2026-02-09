package tobari_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
