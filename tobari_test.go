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
	defer os.RemoveAll(tobariBin)

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
