package madeup

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// What goes over the wire, in the shapes Claude Code sends.
//
// Hook payloads copy the field names, and their order, of the payloads recorded from Claude Code
// 2.1.281 to 2.1.291 (test/fixtures/hooks/). The values are invented, and wherever a real payload
// carries content (a prompt, a command line, a path, a reply) the made-up one carries filler, which
// the collector drops as it drops the real thing. SessionStart is never sent: its HTTP hook has
// never run in a real session.
//
// OTel goes as OTLP/HTTP JSON. Every attribute value is a string, as Claude Code sends them.

// obj is a JSON object that keeps its keys in the order they were added.
type obj []field

type field struct {
	k string
	v any
}

func (o obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.k)
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(f.v)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// OTLP/HTTP JSON, as much of it as the made-up organization sends.

type kv struct {
	Key   string `json:"key"`
	Value value  `json:"value"`
}

type value struct {
	StringValue string `json:"stringValue"`
}

func attrs(pairs ...string) []kv {
	out := make([]kv, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, kv{pairs[i], value{pairs[i+1]}})
	}
	return out
}

type logRecord struct {
	TimeUnixNano         string `json:"timeUnixNano"`
	ObservedTimeUnixNano string `json:"observedTimeUnixNano"`
	Body                 value  `json:"body"`
	Attributes           []kv   `json:"attributes"`
}

type resource struct {
	Attributes []kv `json:"attributes"`
}

type scope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type scopeLogs struct {
	Scope      scope       `json:"scope"`
	LogRecords []logRecord `json:"logRecords"`
}

type resourceLogs struct {
	Resource  resource    `json:"resource"`
	ScopeLogs []scopeLogs `json:"scopeLogs"`
}

type logsRequest struct {
	ResourceLogs []resourceLogs `json:"resourceLogs"`
}

type dataPoint struct {
	AsDouble          float64 `json:"asDouble"`
	StartTimeUnixNano string  `json:"startTimeUnixNano"`
	TimeUnixNano      string  `json:"timeUnixNano"`
	Attributes        []kv    `json:"attributes"`
}

type sum struct {
	AggregationTemporality int         `json:"aggregationTemporality"`
	IsMonotonic            bool        `json:"isMonotonic"`
	DataPoints             []dataPoint `json:"dataPoints"`
}

type metric struct {
	Name string `json:"name"`
	Unit string `json:"unit"`
	Sum  sum    `json:"sum"`
}

type scopeMetrics struct {
	Scope   scope    `json:"scope"`
	Metrics []metric `json:"metrics"`
}

type resourceMetrics struct {
	Resource     resource       `json:"resource"`
	ScopeMetrics []scopeMetrics `json:"scopeMetrics"`
}

type metricsRequest struct {
	ResourceMetrics []resourceMetrics `json:"resourceMetrics"`
}

const (
	logScope    = "com.anthropic.claude_code.events"
	metricScope = "com.anthropic.claude_code"
	// OTLP's delta temporality, which the managed-settings bundle asks Claude Code for.
	deltaTemporality = 1
)

func nanos(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }

func itoa(n int) string { return strconv.Itoa(n) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
