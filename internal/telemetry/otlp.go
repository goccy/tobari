package telemetry

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// The types below mirror the subset of the OTLP protobuf messages tobari
// emits, following the proto3 JSON mapping OTLP/JSON prescribes: trace and
// span ids are hex strings, 64-bit integers are decimal strings, and enums
// are their numeric values.

const (
	scopeName = "github.com/goccy/tobari"

	spanKindInternal = 1
	statusCodeError  = 2
)

type anyValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
	IntValue    *int64  `json:"intValue,omitempty,string"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type otlpResource struct {
	Attributes []keyValue `json:"attributes,omitempty"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type tracesData struct {
	ResourceSpans []resourceSpans `json:"resourceSpans"`
}

type resourceSpans struct {
	Resource   otlpResource `json:"resource"`
	ScopeSpans []scopeSpans `json:"scopeSpans"`
}

type scopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID           string      `json:"traceId"`
	SpanID            string      `json:"spanId"`
	ParentSpanID      string      `json:"parentSpanId,omitempty"`
	Name              string      `json:"name"`
	Kind              int         `json:"kind"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
	Attributes        []keyValue  `json:"attributes,omitempty"`
	Status            *otlpStatus `json:"status,omitempty"`
}

type otlpStatus struct {
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}

type metricsData struct {
	ResourceMetrics []resourceMetrics `json:"resourceMetrics"`
}

type resourceMetrics struct {
	Resource     otlpResource   `json:"resource"`
	ScopeMetrics []scopeMetrics `json:"scopeMetrics"`
}

type scopeMetrics struct {
	Scope   otlpScope    `json:"scope"`
	Metrics []otlpMetric `json:"metrics"`
}

type otlpMetric struct {
	Name  string    `json:"name"`
	Unit  string    `json:"unit"`
	Gauge otlpGauge `json:"gauge"`
}

type otlpGauge struct {
	DataPoints []numberDataPoint `json:"dataPoints"`
}

type numberDataPoint struct {
	Attributes   []keyValue `json:"attributes,omitempty"`
	TimeUnixNano string     `json:"timeUnixNano"`
	AsDouble     float64    `json:"asDouble"`
}

func hexID(id []byte) string {
	return hex.EncodeToString(id)
}

func unixNano(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}

func keyValues(attrs []Attr) []keyValue {
	if len(attrs) == 0 {
		return nil
	}
	kvs := make([]keyValue, len(attrs))
	for i, attr := range attrs {
		kvs[i] = keyValue{Key: attr.key, Value: attr.value}
	}
	return kvs
}

// tracesData encodes the recorded spans. The caller holds r.mu.
func (r *Recorder) tracesData() tracesData {
	spans := make([]otlpSpan, len(r.spans))
	for i, span := range r.spans {
		out := otlpSpan{
			TraceID:           hexID(r.traceID[:]),
			SpanID:            hexID(span.id[:]),
			Name:              span.name,
			Kind:              spanKindInternal,
			StartTimeUnixNano: unixNano(span.start),
			EndTimeUnixNano:   unixNano(span.end),
			Attributes:        keyValues(span.attrs),
		}
		if !span.parent.isZero() {
			out.ParentSpanID = hexID(span.parent[:])
		}
		if span.failure != "" {
			out.Status = &otlpStatus{Code: statusCodeError, Message: span.failure}
		}
		spans[i] = out
	}
	return tracesData{ResourceSpans: []resourceSpans{{
		Resource:   otlpResource{Attributes: keyValues(r.resource)},
		ScopeSpans: []scopeSpans{{Scope: otlpScope{Name: scopeName}, Spans: spans}},
	}}}
}

// metricsData encodes the recorded points, one gauge per metric name in
// first-recorded order. The caller holds r.mu.
func (r *Recorder) metricsData() metricsData {
	var (
		metrics []otlpMetric
		index   = make(map[string]int)
	)
	for _, p := range r.points {
		i, ok := index[p.name]
		if !ok {
			i = len(metrics)
			index[p.name] = i
			metrics = append(metrics, otlpMetric{Name: p.name, Unit: p.unit})
		}
		metrics[i].Gauge.DataPoints = append(metrics[i].Gauge.DataPoints, numberDataPoint{
			Attributes:   keyValues(p.attrs),
			TimeUnixNano: unixNano(p.time),
			AsDouble:     p.value,
		})
	}
	return metricsData{ResourceMetrics: []resourceMetrics{{
		Resource:     otlpResource{Attributes: keyValues(r.resource)},
		ScopeMetrics: []scopeMetrics{{Scope: otlpScope{Name: scopeName}, Metrics: metrics}},
	}}}
}

// writeLine writes v as a single JSON line, the unit the otlpjsonfile format
// is made of.
func writeLine(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to encode telemetry: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write telemetry: %w", err)
	}
	return nil
}
