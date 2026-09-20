package tobari_test

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// otlpAttrs is the OTLP/JSON attribute list, reduced to the string values the
// tests look at.
type otlpAttrs []struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
	} `json:"value"`
}

func (a otlpAttrs) get(key string) string {
	for _, kv := range a {
		if kv.Key == key {
			return kv.Value.StringValue
		}
	}
	return ""
}

type otlpTestSpan struct {
	TraceID      string    `json:"traceId"`
	SpanID       string    `json:"spanId"`
	ParentSpanID string    `json:"parentSpanId"`
	Name         string    `json:"name"`
	Attributes   otlpAttrs `json:"attributes"`
}

type otlpTestPoint struct {
	Attributes otlpAttrs `json:"attributes"`
	AsDouble   float64   `json:"asDouble"`
}

// readTelemetry decodes every OTLP/JSON line under dir into its spans and its
// metric points by metric name.
func readTelemetry(t *testing.T, dir string) ([]otlpTestSpan, map[string][]otlpTestPoint) {
	t.Helper()
	type line struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []otlpTestSpan `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
		ResourceMetrics []struct {
			ScopeMetrics []struct {
				Metrics []struct {
					Name  string `json:"name"`
					Gauge struct {
						DataPoints []otlpTestPoint `json:"dataPoints"`
					} `json:"gauge"`
				} `json:"metrics"`
			} `json:"scopeMetrics"`
		} `json:"resourceMetrics"`
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var spans []otlpTestSpan
	points := make(map[string][]otlpTestPoint)
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(nil, 64<<20)
		for scanner.Scan() {
			var l line
			if err := json.Unmarshal(scanner.Bytes(), &l); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			for _, rs := range l.ResourceSpans {
				for _, ss := range rs.ScopeSpans {
					spans = append(spans, ss.Spans...)
				}
			}
			for _, rm := range l.ResourceMetrics {
				for _, sm := range rm.ScopeMetrics {
					for _, m := range sm.Metrics {
						points[m.Name] = append(points[m.Name], m.Gauge.DataPoints...)
					}
				}
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return spans, points
}

// TestBuildTelemetry verifies that TOBARI_OTEL_DIR makes a build report, as one
// trace, every tool invocation and every stage of the dependency analysis,
// together with the per-package analysis cost, without changing the analysis
// result.
func TestBuildTelemetry(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	otelDir := filepath.Join(t.TempDir(), "otel")
	bin := filepath.Join(t.TempDir(), "app")
	cmd := exec.CommandContext(ctx, "go", "build", "-cover", "-toolexec="+tobariBin, "-o", bin, ".")
	cmd.Dir = "testdata/crosspkg"
	cmd.Env = append(os.Environ(),
		"GOCACHE="+t.TempDir(),
		"TMPDIR="+t.TempDir(),
		"TOBARI_OTEL_DIR="+otelDir,
		// The trace must not depend on what the environment running the
		// tests happens to propagate.
		"TRACEPARENT=",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %s: %v", string(out), err)
	}

	if got, want := extractSuppDeps(t, bin), buildCrosspkgSuppDeps(t, ctx, tobariBin, ""); got != want {
		t.Errorf("telemetry changed suppDeps\nwithout: %s\nwith:    %s", want, got)
	}

	spans, points := readTelemetry(t, otelDir)
	if len(spans) == 0 {
		t.Fatal("no spans were written")
	}

	t.Run("one trace without dangling parents", func(t *testing.T) {
		ids := make(map[string]struct{}, len(spans))
		for _, span := range spans {
			ids[span.SpanID] = struct{}{}
		}
		for _, span := range spans {
			if span.TraceID != spans[0].TraceID {
				t.Fatalf("span %q is in trace %s, others in %s", span.Name, span.TraceID, spans[0].TraceID)
			}
			if span.ParentSpanID == "" {
				continue
			}
			if _, ok := ids[span.ParentSpanID]; !ok {
				t.Errorf("span %q names a parent that was never written", span.Name)
			}
		}
	})

	t.Run("tool invocations", func(t *testing.T) {
		tools := make(map[string]struct{})
		for _, span := range spans {
			if span.Name == "tobari.tool" {
				tools[span.Attributes.get("tobari.tool")] = struct{}{}
			}
		}
		for _, tool := range []string{"cover", "compile", "link", "packagesdriver"} {
			if _, ok := tools[tool]; !ok {
				t.Errorf("no span for tool %q (got %v)", tool, tools)
			}
		}
	})

	t.Run("analysis stages", func(t *testing.T) {
		var analysis []otlpTestSpan
		for _, span := range spans {
			if span.Name == "tobari.analysis" {
				analysis = append(analysis, span)
			}
		}
		if len(analysis) != 1 {
			t.Fatalf("expected one analysis, got %d", len(analysis))
		}
		var stages []string
		for _, span := range spans {
			if span.ParentSpanID == analysis[0].SpanID {
				stages = append(stages, span.Name)
			}
		}
		slices.Sort(stages)
		want := []string{
			"tobari.analysis.dependencies",
			"tobari.analysis.followable_callees",
			"tobari.analysis.frontiers",
			"tobari.analysis.go_list",
			"tobari.analysis.load",
			"tobari.analysis.rta",
			"tobari.analysis.ssa_build",
			"tobari.analysis.ssa_create",
			"tobari.analysis.vta",
		}
		if !slices.Equal(stages, want) {
			t.Errorf("stages:\ngot  %v\nwant %v", stages, want)
		}
	})

	t.Run("per-package cost", func(t *testing.T) {
		wantModules := map[string]string{
			"example.com/crosspkg/transform": "example.com/crosspkg",
			"fmt":                            "std",
		}
		for _, metric := range []string{
			"tobari.analysis.package.ssa_build.duration",
			"tobari.analysis.package.reachable_functions",
			"tobari.analysis.package.call_edges",
		} {
			modules := make(map[string]string)
			for _, point := range points[metric] {
				modules[point.Attributes.get("tobari.package")] = point.Attributes.get("tobari.module")
			}
			for pkg, want := range wantModules {
				if got, ok := modules[pkg]; !ok || got != want {
					t.Errorf("%s: package %q: got module %q (reported: %v), want %q", metric, pkg, got, ok, want)
				}
			}
		}
	})
}

// TestBuildTelemetryReportsSkippedPackages verifies that the packages
// --exclude-analysis keeps out of the analysis are reported as skipped and are
// not reported as built.
func TestBuildTelemetryReportsSkippedPackages(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	otelDir := filepath.Join(t.TempDir(), "otel")
	cmd := exec.CommandContext(ctx, "go", "build", "-cover",
		"-toolexec="+tobariBin+" --exclude-analysis=strconv",
		"-o", filepath.Join(t.TempDir(), "app"), ".")
	cmd.Dir = "testdata/crosspkg"
	cmd.Env = append(os.Environ(),
		"GOCACHE="+t.TempDir(),
		"TMPDIR="+t.TempDir(),
		"TOBARI_OTEL_DIR="+otelDir,
		"TRACEPARENT=",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %s: %v", string(out), err)
	}

	_, points := readTelemetry(t, otelDir)
	packagesOf := func(metric string) []string {
		var pkgs []string
		for _, point := range points[metric] {
			pkgs = append(pkgs, point.Attributes.get("tobari.package"))
		}
		slices.Sort(pkgs)
		return pkgs
	}
	if got, want := packagesOf("tobari.analysis.package.skipped"), []string{"strconv"}; !slices.Equal(got, want) {
		t.Errorf("skipped packages: got %v, want %v", got, want)
	}
	if slices.Contains(packagesOf("tobari.analysis.package.ssa_build.duration"), "strconv") {
		t.Error("a skipped package was reported as built")
	}
}
