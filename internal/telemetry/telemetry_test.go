package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// readLines decodes the single-line OTLP/JSON files matching pattern in dir.
func readLines[T any](t *testing.T, dir, pattern string) []T {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	var ret []T
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		ret = append(ret, v)
	}
	return ret
}

func TestDisabled(t *testing.T) {
	t.Setenv(EnvDir, "")
	rec, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Fatal("expected no recorder when the directory is unset")
	}

	// Every entry point must be usable without a recorder.
	ctx := WithRecorder(context.Background(), rec)
	if Enabled(ctx) {
		t.Fatal("expected telemetry to be disabled")
	}
	ctx, span := Start(ctx, "op")
	span.SetAttrs(String("k", "v"))
	span.Fail(errors.New("failed"))
	span.End()
	RecordSeconds(ctx, "m", time.Second)
	if got := Traceparent(ctx); got != "" {
		t.Fatalf("unexpected traceparent %q", got)
	}
	if err := Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRelativeDirIsRejected(t *testing.T) {
	t.Setenv(EnvDir, "otel")
	if _, err := FromEnv(); !errors.Is(err, ErrRelativeDir) {
		t.Fatalf("expected ErrRelativeDir, got %v", err)
	}
}

func TestFlushWritesOTLPJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "otel")
	t.Setenv(EnvDir, dir)
	t.Setenv(EnvTraceparent, "00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01")

	rec, err := FromEnv(String("service.name", "tobari"))
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(100, 0)
	rec.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}

	ctx := WithRecorder(context.Background(), rec)
	ctx, parent := Start(ctx, "parent", Int("n", 3))
	childCtx, child := Start(ctx, "child")
	child.Fail(errors.New("boom"))
	child.End()
	child.End() // must not move the end time
	_, open := Start(childCtx, "left open")
	RecordSeconds(ctx, "duration", 1500*time.Millisecond, String("pkg", "a"))
	RecordCount(ctx, "count", 7, String("pkg", "a"))
	RecordSeconds(ctx, "duration", 2*time.Second, String("pkg", "b"))
	parent.SetAttrs(Bool("done", true))
	parent.End()

	if want := "00-0102030405060708090a0b0c0d0e0f10-" + hexID(child.id[:]) + "-01"; Traceparent(childCtx) != want {
		t.Fatalf("traceparent: got %q, want %q", Traceparent(childCtx), want)
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}

	str := func(s string) anyValue { return anyValue{StringValue: &s} }
	n3, done := int64(3), true

	traces := readLines[tracesData](t, dir, "tobari_traces_*.jsonl")
	wantTraces := []tracesData{{ResourceSpans: []resourceSpans{{
		Resource: otlpResource{Attributes: []keyValue{{Key: "service.name", Value: str("tobari")}}},
		ScopeSpans: []scopeSpans{{
			Scope: otlpScope{Name: scopeName},
			Spans: []otlpSpan{
				{
					TraceID:           "0102030405060708090a0b0c0d0e0f10",
					SpanID:            hexID(parent.id[:]),
					ParentSpanID:      "1112131415161718",
					Name:              "parent",
					Kind:              spanKindInternal,
					StartTimeUnixNano: "101000000000",
					EndTimeUnixNano:   "108000000000",
					Attributes: []keyValue{
						{Key: "n", Value: anyValue{IntValue: &n3}},
						{Key: "done", Value: anyValue{BoolValue: &done}},
					},
				},
				{
					TraceID:           "0102030405060708090a0b0c0d0e0f10",
					SpanID:            hexID(child.id[:]),
					ParentSpanID:      hexID(parent.id[:]),
					Name:              "child",
					Kind:              spanKindInternal,
					StartTimeUnixNano: "102000000000",
					EndTimeUnixNano:   "103000000000",
					Status:            &otlpStatus{Code: statusCodeError, Message: "boom"},
				},
				{
					// Never ended by the caller: Flush closes it.
					TraceID:           "0102030405060708090a0b0c0d0e0f10",
					SpanID:            hexID(open.id[:]),
					ParentSpanID:      hexID(child.id[:]),
					Name:              "left open",
					Kind:              spanKindInternal,
					StartTimeUnixNano: "104000000000",
					EndTimeUnixNano:   "109000000000",
				},
			},
		}},
	}}}}
	if diff := cmp.Diff(wantTraces, traces); diff != "" {
		t.Fatalf("traces (-want +got):\n%s", diff)
	}

	metrics := readLines[metricsData](t, dir, "tobari_metrics_*.jsonl")
	pkg := func(v string) []keyValue { return []keyValue{{Key: "pkg", Value: str(v)}} }
	wantMetrics := []metricsData{{ResourceMetrics: []resourceMetrics{{
		Resource: otlpResource{Attributes: []keyValue{{Key: "service.name", Value: str("tobari")}}},
		ScopeMetrics: []scopeMetrics{{
			Scope: otlpScope{Name: scopeName},
			Metrics: []otlpMetric{
				{Name: "duration", Unit: "s", Gauge: otlpGauge{DataPoints: []numberDataPoint{
					{Attributes: pkg("a"), TimeUnixNano: "105000000000", AsDouble: 1.5},
					{Attributes: pkg("b"), TimeUnixNano: "107000000000", AsDouble: 2},
				}}},
				{Name: "count", Unit: "1", Gauge: otlpGauge{DataPoints: []numberDataPoint{
					{Attributes: pkg("a"), TimeUnixNano: "106000000000", AsDouble: 7},
				}}},
			},
		}},
	}}}}
	if diff := cmp.Diff(wantMetrics, metrics); diff != "" {
		t.Fatalf("metrics (-want +got):\n%s", diff)
	}

	// A second flush has nothing left to write.
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := len(readLines[tracesData](t, dir, "tobari_traces_*.jsonl")); got != 1 {
		t.Fatalf("expected the second flush to write nothing, got %d trace files", got)
	}
}

// TestWireFormat pins the proto3 JSON details a struct comparison cannot see:
// 64-bit integers are strings and ids are hex.
func TestWireFormat(t *testing.T) {
	n := int64(42)
	got, err := json.Marshal(keyValue{Key: "k", Value: anyValue{IntValue: &n}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"key":"k","value":{"intValue":"42"}}`; string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestTraceIDIsSharedByProcessesOfOneParent(t *testing.T) {
	t.Setenv(EnvDir, t.TempDir())
	t.Setenv(EnvTraceparent, "")
	a, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	b, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if a.traceID.isZero() || a.traceID != b.traceID {
		t.Fatalf("expected one non-zero trace id, got %x and %x", a.traceID, b.traceID)
	}
	if !a.parent.isZero() {
		t.Fatalf("expected no parent span, got %x", a.parent)
	}
}

func TestParseTraceparent(t *testing.T) {
	const (
		tid = "0102030405060708090a0b0c0d0e0f10"
		sid = "1112131415161718"
	)
	for _, test := range []struct {
		name  string
		value string
		ok    bool
	}{
		{name: "valid", value: "00-" + tid + "-" + sid + "-01", ok: true},
		{name: "unsampled", value: "00-" + tid + "-" + sid + "-00", ok: true},
		{name: "empty", value: ""},
		{name: "unknown version", value: "01-" + tid + "-" + sid + "-01"},
		{name: "missing flags", value: "00-" + tid + "-" + sid},
		{name: "extra field", value: "00-" + tid + "-" + sid + "-01-x"},
		{name: "short trace id", value: "00-" + tid[2:] + "-" + sid + "-01"},
		{name: "long span id", value: "00-" + tid + "-" + sid + "00-01"},
		{name: "non-hex trace id", value: "00-" + "zz" + tid[2:] + "-" + sid + "-01"},
		{name: "non-hex flags", value: "00-" + tid + "-" + sid + "-zz"},
		{name: "zero trace id", value: "00-00000000000000000000000000000000-" + sid + "-01"},
		{name: "zero span id", value: "00-" + tid + "-0000000000000000-01"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotTID, gotSID, ok := parseTraceparent(test.value)
			if ok != test.ok {
				t.Fatalf("ok: got %v, want %v", ok, test.ok)
			}
			if !ok {
				return
			}
			if hexID(gotTID[:]) != tid || hexID(gotSID[:]) != sid {
				t.Fatalf("got %x %x", gotTID, gotSID)
			}
		})
	}
}
