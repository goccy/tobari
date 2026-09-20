// Package telemetry records where a tobari-instrumented build spends its time
// and writes the result as OpenTelemetry data.
//
// A `go build` runs tobari once per tool invocation, as many short-lived
// processes. Each process that has telemetry enabled collects its spans and
// metric points in memory and writes them on exit to its own files under the
// directory named by TOBARI_OTEL_DIR, in the OTLP/JSON line format the
// OpenTelemetry Collector's otlpjsonfile receiver reads. One file per process
// means no coordination between processes is needed.
//
// The OTLP/JSON encoding is written by hand instead of through the
// OpenTelemetry SDK: the tobari module is a dependency of every instrumented
// program, so anything it requires lands in the module graph of its users.
//
// A nil *Recorder and a nil *Span are valid and do nothing, so call sites do
// not need to check whether telemetry is enabled.
package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// EnvDir names the directory telemetry files are written to. Telemetry is
	// disabled when it is unset.
	EnvDir = "TOBARI_OTEL_DIR"

	// EnvTraceparent carries a W3C Trace Context traceparent header value
	// between processes, so that the tobari processes of one build join the
	// same trace and a caller can place the whole build under its own span.
	EnvTraceparent = "TRACEPARENT"

	tracesFilePattern  = "tobari_traces_%d_%d.jsonl"
	metricsFilePattern = "tobari_metrics_%d_%d.jsonl"
)

// ErrRelativeDir is returned when TOBARI_OTEL_DIR is not an absolute path.
// The tools of a build run in different working directories, so a relative
// path would scatter the files across the package directories.
var ErrRelativeDir = errors.New(EnvDir + " must be an absolute path")

type (
	traceID [16]byte
	spanID  [8]byte
)

func (id traceID) isZero() bool { return id == traceID{} }
func (id spanID) isZero() bool  { return id == spanID{} }

// Attr is a key/value pair attached to a span, a metric point or the resource.
type Attr struct {
	key   string
	value anyValue
}

func String(key, value string) Attr { return Attr{key: key, value: anyValue{StringValue: &value}} }
func Bool(key string, value bool) Attr {
	return Attr{key: key, value: anyValue{BoolValue: &value}}
}

func Int(key string, value int) Attr {
	v := int64(value)
	return Attr{key: key, value: anyValue{IntValue: &v}}
}

// Recorder collects the telemetry of one process.
type Recorder struct {
	dir      string
	traceID  traceID
	parent   spanID
	resource []Attr
	now      func() time.Time

	mu     sync.Mutex
	spans  []*Span
	points []point
}

// Span is a timed operation. It is created by Start and finished by End.
type Span struct {
	rec    *Recorder
	id     spanID
	parent spanID
	name   string
	start  time.Time

	// The fields below are guarded by rec.mu.
	end     time.Time
	attrs   []Attr
	failure string
}

type point struct {
	name  string
	unit  string
	value float64
	time  time.Time
	attrs []Attr
}

// FromEnv returns the recorder configured by the environment, or nil when
// telemetry is disabled.
//
// The go command starts every tool of a build itself, so all tobari processes
// of one build share their parent process. Deriving the trace ID from the
// parent's pid therefore puts them in one trace without any channel between
// them, whichever tool they wrap. Pids are reused over time, so two builds
// can end up with the same trace ID; their spans are then still apart in
// time. When TRACEPARENT is set it takes precedence, which is how a process
// started by tobari joins its parent, and how a caller that traces the whole
// build gets a trace ID of its own choosing.
func FromEnv(resource ...Attr) (*Recorder, error) {
	dir := os.Getenv(EnvDir)
	if dir == "" {
		return nil, nil
	}
	if !filepath.IsAbs(dir) {
		return nil, ErrRelativeDir
	}
	rec := &Recorder{dir: dir, resource: resource, now: time.Now}
	if tid, sid, ok := parseTraceparent(os.Getenv(EnvTraceparent)); ok {
		rec.traceID, rec.parent = tid, sid
		return rec, nil
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "tobari build of go command pid %d", os.Getppid()))
	copy(rec.traceID[:], sum[:])
	return rec, nil
}

// parseTraceparent parses a version 00 traceparent value as defined by W3C
// Trace Context: "00-<32 hex trace id>-<16 hex parent id>-<2 hex flags>", with
// all-zero ids being invalid. Any other input is rejected.
func parseTraceparent(v string) (traceID, spanID, bool) {
	parts := strings.Split(v, "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[3]) != 2 {
		return traceID{}, spanID{}, false
	}
	var (
		tid traceID
		sid spanID
	)
	if len(parts[1]) != hex.EncodedLen(len(tid)) || len(parts[2]) != hex.EncodedLen(len(sid)) {
		return traceID{}, spanID{}, false
	}
	if _, err := hex.Decode(tid[:], []byte(parts[1])); err != nil {
		return traceID{}, spanID{}, false
	}
	if _, err := hex.Decode(sid[:], []byte(parts[2])); err != nil {
		return traceID{}, spanID{}, false
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return traceID{}, spanID{}, false
	}
	if tid.isZero() || sid.isZero() {
		return traceID{}, spanID{}, false
	}
	return tid, sid, true
}

type (
	recorderKey struct{}
	spanKey     struct{}
)

// WithRecorder returns a context that carries rec.
func WithRecorder(ctx context.Context, rec *Recorder) context.Context {
	if rec == nil {
		return ctx
	}
	return context.WithValue(ctx, recorderKey{}, rec)
}

func recorderFrom(ctx context.Context) *Recorder {
	rec, _ := ctx.Value(recorderKey{}).(*Recorder)
	return rec
}

// Enabled reports whether ctx carries a recorder. It lets a caller skip work
// that only produces telemetry.
func Enabled(ctx context.Context) bool {
	return recorderFrom(ctx) != nil
}

// Start begins a span named name as a child of the span carried by ctx, and
// returns a context carrying the new span.
func Start(ctx context.Context, name string, attrs ...Attr) (context.Context, *Span) {
	rec := recorderFrom(ctx)
	if rec == nil {
		return ctx, nil
	}
	span := &Span{rec: rec, name: name, start: rec.now(), attrs: attrs, parent: rec.parent}
	if parent, ok := ctx.Value(spanKey{}).(*Span); ok {
		span.parent = parent.id
	}
	// A failure of the system's random source leaves the id zero, which OTLP
	// consumers reject; panicking here would turn a diagnostics problem into
	// a build failure, and crypto/rand does not fail on supported platforms.
	_, _ = rand.Read(span.id[:])

	rec.mu.Lock()
	rec.spans = append(rec.spans, span)
	rec.mu.Unlock()
	return context.WithValue(ctx, spanKey{}, span), span
}

// Flush writes out the telemetry of the recorder carried by ctx, if any.
func Flush(ctx context.Context) error {
	return recorderFrom(ctx).Flush()
}

// SetAttrs adds attributes to the span.
func (s *Span) SetAttrs(attrs ...Attr) {
	if s == nil {
		return
	}
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.attrs = append(s.attrs, attrs...)
}

// Fail marks the span as failed with the given error.
func (s *Span) Fail(err error) {
	if s == nil || err == nil {
		return
	}
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.failure = err.Error()
}

// End finishes the span. Only the first call has an effect.
func (s *Span) End() {
	if s == nil {
		return
	}
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	if s.end.IsZero() {
		s.end = s.rec.now()
	}
}

// Traceparent returns the traceparent value that makes a child process
// continue the trace under the span carried by ctx. It returns "" when
// telemetry is disabled or ctx carries no span.
func Traceparent(ctx context.Context) string {
	span, ok := ctx.Value(spanKey{}).(*Span)
	if !ok {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-01", hexID(span.rec.traceID[:]), hexID(span.id[:]))
}

// RecordSeconds records d as one point of the gauge metric name.
func RecordSeconds(ctx context.Context, name string, d time.Duration, attrs ...Attr) {
	record(ctx, name, "s", d.Seconds(), attrs)
}

// RecordCount records n as one point of the gauge metric name.
func RecordCount(ctx context.Context, name string, n int, attrs ...Attr) {
	record(ctx, name, "1", float64(n), attrs)
}

func record(ctx context.Context, name, unit string, value float64, attrs []Attr) {
	rec := recorderFrom(ctx)
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.points = append(rec.points, point{name: name, unit: unit, value: value, time: rec.now(), attrs: attrs})
}

// Flush writes everything recorded so far and forgets it. Spans that are still
// open are ended first: Flush runs when the process is about to exit, and a
// span that never ended would otherwise be lost.
func (r *Recorder) Flush() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	for _, span := range r.spans {
		if span.end.IsZero() {
			span.end = now
		}
	}
	if len(r.spans) == 0 && len(r.points) == 0 {
		return nil
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", r.dir, err)
	}
	pid, stamp := os.Getpid(), now.UnixNano()
	if len(r.spans) != 0 {
		path := filepath.Join(r.dir, fmt.Sprintf(tracesFilePattern, pid, stamp))
		if err := writeLine(path, r.tracesData()); err != nil {
			return err
		}
		r.spans = nil
	}
	if len(r.points) != 0 {
		path := filepath.Join(r.dir, fmt.Sprintf(metricsFilePattern, pid, stamp))
		if err := writeLine(path, r.metricsData()); err != nil {
			return err
		}
		r.points = nil
	}
	return nil
}
