package tobari_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/tobari/internal/cover"
)

// autoskipProject is a private copy of testdata/autoskip that the test edits
// and rebuilds. Its dependencies are vendored so that they have module
// versions — only such packages are left out by learning — without needing a
// module proxy:
//
//   - gen only handles plain data and is made expensive, so it can be left out
//   - callback calls a function of the program back, so it is on a call path
//   - box only stores and returns a value of the program. It is on no call
//     path, yet the analysis has to follow the value through it
type autoskipProject struct {
	t         *testing.T
	ctx       context.Context
	tobariBin string
	dir       string
	coverDir  string
	tmpDir    string
	goCache   string
	builds    int
}

func newAutoskipProject(t *testing.T) *autoskipProject {
	t.Helper()
	ctx := t.Context()
	p := &autoskipProject{
		t:         t,
		ctx:       ctx,
		tobariBin: filepath.Join(t.TempDir(), "tobari"),
		dir:       filepath.Join(t.TempDir(), "autoskip"),
		coverDir:  t.TempDir(),
		tmpDir:    t.TempDir(),
		goCache:   t.TempDir(),
	}
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", p.tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}
	if err := os.CopyFS(p.dir, os.DirFS("testdata/autoskip")); err != nil {
		t.Fatal(err)
	}

	// A few thousand functions calling each other give gen a share of the
	// analysis that is worth verifying.
	var heavy bytes.Buffer
	const n = 3000
	heavy.WriteString("package gen\n\nfunc heavy(n int) int { return g0(n) }\n")
	for i := range n {
		fmt.Fprintf(&heavy, "\nfunc g%d(n int) int {\n\tif n <= 0 {\n\t\treturn %d\n\t}\n\treturn 0", i, i)
		for _, callee := range []int{i + 1, i + 2} {
			if callee < n {
				fmt.Fprintf(&heavy, " + g%d(n-1)", callee)
			}
		}
		heavy.WriteString("\n}\n")
	}
	p.write("vendor/example.com/gen/heavy.go", heavy.String())
	return p
}

func (p *autoskipProject) write(name, content string) {
	p.t.Helper()
	if err := os.WriteFile(filepath.Join(p.dir, name), []byte(content), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

// edit replaces old, which must occur exactly once, in a source file.
func (p *autoskipProject) edit(name, old, replacement string) {
	p.t.Helper()
	data, err := os.ReadFile(filepath.Join(p.dir, name))
	if err != nil {
		p.t.Fatal(err)
	}
	if strings.Count(string(data), old) != 1 {
		p.t.Fatalf("%s: expected exactly one %q", name, old)
	}
	p.write(name, strings.Replace(string(data), old, replacement, 1))
}

// build builds the project and returns the binary and the combined output.
//
// The builds of a project share a Go build cache, so that the standard library
// is compiled once rather than once per build. The analysis only runs when the
// main package is rebuilt, which a new comment in main.go forces; comments do
// not reach the analysis, so nothing else changes.
func (p *autoskipProject) build(env ...string) (string, string, error) {
	p.t.Helper()
	p.builds++
	mainSrc, err := os.ReadFile("testdata/autoskip/main.go")
	if err != nil {
		p.t.Fatal(err)
	}
	p.write("main.go", fmt.Sprintf("%s\n// build %d\n", mainSrc, p.builds))

	bin := filepath.Join(p.t.TempDir(), "app")
	cmd := exec.CommandContext(p.ctx, "go", "build", "-cover", "-toolexec="+p.tobariBin, "-o", bin, ".")
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(),
		"GOCACHE="+p.goCache,
		"TMPDIR="+p.tmpDir,
		"TOBARI_COVERDIR="+p.coverDir,
		"TOBARI_ANALYSIS=",
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return bin, string(out), err
}

func (p *autoskipProject) mustBuild(env ...string) string {
	p.t.Helper()
	bin, out, err := p.build(env...)
	if err != nil {
		p.t.Fatalf("go build failed: %s: %v", out, err)
	}
	return bin
}

// packages returns the packages of the last analysis report by import path.
func (p *autoskipProject) packages() map[string]*cover.AnalysisPackage {
	p.t.Helper()
	reports, err := cover.ReadAnalysisReports(filepath.Join(p.coverDir, "tobari", "analysis"))
	if err != nil {
		p.t.Fatal(err)
	}
	if len(reports) != 1 {
		p.t.Fatalf("expected one report, got %d", len(reports))
	}
	pkgs := make(map[string]*cover.AnalysisPackage)
	for _, pkg := range reports[0].Packages {
		pkgs[pkg.Package] = pkg
	}
	return pkgs
}

func (p *autoskipProject) recordPath() string {
	return filepath.Join(p.coverDir, "tobari", "analysis", "learned", "example.com%2Fautoskip.json")
}

// suppDeps returns the dependency map embedded in a built binary.
func (p *autoskipProject) suppDeps(bin string) map[string][]string {
	p.t.Helper()
	raw, found := findSuppDepsOf(p.t, bin, "example.com/autoskip")
	if !found {
		p.t.Fatal("suppDeps not found in binary")
	}
	var deps map[string][]string
	if err := json.Unmarshal([]byte(raw), &deps); err != nil {
		p.t.Fatal(err)
	}
	return deps
}

type packageState struct {
	Skipped, Learned, OnPath bool
	Verdict                  cover.SkipVerdict
}

func (p *autoskipProject) expect(want map[string]packageState) {
	p.t.Helper()
	pkgs := p.packages()
	for pkgPath, wantState := range want {
		pkg, ok := pkgs[pkgPath]
		if !ok {
			p.t.Errorf("package %q is missing from the report", pkgPath)
			continue
		}
		got := packageState{Skipped: pkg.Skipped, Learned: pkg.Learned, OnPath: pkg.OnPath, Verdict: pkg.Verdict}
		if got != wantState {
			p.t.Errorf("package %q: got %+v, want %+v", pkgPath, got, wantState)
		}
	}
}

// TestLearnedSkips follows a target through the builds that matter to the
// learned skips: learning, reuse across an edit that leaves the use of the
// dependencies alone, and re-analysis after an edit that changes it. At every
// step the result must be the one a full analysis produces.
func TestLearnedSkips(t *testing.T) {
	t.Parallel() // see TestBuildTelemetry
	p := newAutoskipProject(t)
	equalsFullAnalysis := func(bin string) {
		t.Helper()
		full := p.suppDeps(p.mustBuild("TOBARI_ANALYSIS=full"))
		if got := p.suppDeps(bin); !equalDeps(got, full) {
			t.Errorf("result differs from a full analysis\nfull: %v\ngot:  %v", full, got)
		}
	}

	// Learning. Everything is analyzed; gen and box are on no call path and
	// are put to the test. Leaving gen out changes nothing. Leaving box out
	// loses the Task that Resume runs.
	bin := p.mustBuild()
	p.expect(map[string]packageState{
		"example.com/gen":      {Verdict: cover.VerdictSkip},
		"example.com/box":      {Verdict: cover.VerdictKeep},
		"example.com/callback": {OnPath: true},
	})
	deps := p.suppDeps(bin)
	if got := deps["example.com/autoskip/app.Resume"]; !contains(got, "(example.com/autoskip/app.Task).Run") {
		t.Errorf("Resume must depend on Task.Run, got %v", got)
	}
	equalsFullAnalysis(bin)

	// An edit of a function body. Nothing about the use of the dependencies
	// changed, so gen stays out.
	p.edit("app/app.go", "return n + 1", "return n + 2")
	bin = p.mustBuild()
	p.expect(map[string]packageState{
		"example.com/gen":      {Skipped: true, Learned: true, Verdict: cover.VerdictSkip},
		"example.com/box":      {Verdict: cover.VerdictKeep},
		"example.com/callback": {OnPath: true},
	})
	equalsFullAnalysis(bin)

	// A full analysis on request ignores what was learned, and keeps it.
	p.mustBuild("TOBARI_ANALYSIS=full")
	p.expect(map[string]packageState{
		"example.com/gen":      {},
		"example.com/box":      {},
		"example.com/callback": {OnPath: true},
	})
	if _, err := os.Stat(p.recordPath()); err != nil {
		t.Errorf("a full analysis must leave the learned skips alone: %v", err)
	}

	// The program starts handing gen a function. The way gen is used changed,
	// so gen is analyzed again, which finds the new call path through it.
	p.edit("app/app.go", "return helper(gen.Sum(n))", "return gen.Apply(gen.Sum(n), helper)")
	bin = p.mustBuild()
	p.expect(map[string]packageState{
		"example.com/gen":      {OnPath: true},
		"example.com/box":      {Verdict: cover.VerdictKeep},
		"example.com/callback": {OnPath: true},
	})
	deps = p.suppDeps(bin)
	if got := deps["example.com/autoskip/app.Compute"]; !contains(got, "example.com/autoskip/app.helper") {
		t.Errorf("Compute must depend on helper through gen.Apply, got %v", got)
	}
	equalsFullAnalysis(bin)
}

// TestLearnedSkipsVerify checks that TOBARI_ANALYSIS=verify accepts learned
// skips that hold and rejects — and discards — ones that do not.
func TestLearnedSkipsVerify(t *testing.T) {
	t.Parallel() // see TestBuildTelemetry
	p := newAutoskipProject(t)
	p.mustBuild()

	if _, out, err := p.build("TOBARI_ANALYSIS=verify"); err != nil {
		t.Fatalf("verifying what was just learned failed: %s: %v", out, err)
	}
	p.expect(map[string]packageState{"example.com/gen": {}})

	// Claim that box can be left out, which the learning build disproved.
	data, err := os.ReadFile(p.recordPath())
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), `"example.com/box": "keep"`, `"example.com/box": "skip"`, 1)
	if tampered == string(data) {
		t.Fatalf("box has no keep verdict to tamper with:\n%s", data)
	}
	if err := os.WriteFile(p.recordPath(), []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	_, out, err := p.build("TOBARI_ANALYSIS=verify")
	if err == nil {
		t.Fatal("expected the build to fail")
	}
	if !strings.Contains(out, "the learned skips of example.com/autoskip change the result") {
		t.Errorf("unexpected output: %s", out)
	}
	if _, err := os.Stat(p.recordPath()); !os.IsNotExist(err) {
		t.Errorf("the disproved learned skips must be discarded, got %v", err)
	}

	if _, out, err := p.build("TOBARI_ANALYSIS=bogus"); err == nil || !strings.Contains(out, "TOBARI_ANALYSIS must be") {
		t.Errorf("expected an invalid mode to be rejected, got %v: %s", err, out)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func equalDeps(a, b map[string][]string) bool {
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(x, y)
}
