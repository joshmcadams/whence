package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/joshmcadams/whence/internal/model"
)

func TestHumanUptime(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "-"},
		{-5 * time.Second, "-"},
		{45 * time.Second, "45s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"},   // exactly a minute rolls to minutes
		{90 * time.Second, "1m"},   // sub-minute remainder is dropped
		{59 * time.Minute, "59m"},  // just under an hour
		{60 * time.Minute, "1h0m"}, // exactly an hour
		{3*time.Hour + 17*time.Minute, "3h17m"},
		{24 * time.Hour, "1d0h"}, // exactly a day
		{2*24*time.Hour + 4*time.Hour, "2d4h"},
	}
	for _, tc := range cases {
		if got := HumanUptime(tc.d); got != tc.want {
			t.Errorf("HumanUptime(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hi", 5, "hi"},             // shorter than limit, unchanged
		{"hello", 5, "hello"},       // exactly the limit, unchanged
		{"hello world", 5, "hell…"}, // cut adds an ellipsis (n-1 runes + …)
		{"hello", 1, "h"},           // n<=1 has no room for an ellipsis
		{"hello", 0, ""},
		{"héllo", 5, "héllo"}, // multi-byte but 5 runes: unchanged
		{"日本語テスト", 3, "日本…"},  // counts runes, not bytes
	}
	for _, tc := range cases {
		if got := Truncate(tc.s, tc.n); got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
		}
	}
}

func TestSrcLabel(t *testing.T) {
	if got := SrcLabel(model.SourceDocker); got != "docker" {
		t.Errorf("docker label = %q", got)
	}
	if got := SrcLabel(model.SourceProcess); got != "proc" {
		t.Errorf("process label = %q", got)
	}
	if got := SrcLabel(model.Source("???")); got != "proc" {
		t.Errorf("unknown source label = %q, want proc (default)", got)
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		name string
		s    model.Server
		want string
	}{
		{"docker", model.Server{Port: 3000, Source: model.SourceDocker, Name: "web-1"}, ":3000 web-1 [container web-1]"},
		{"process", model.Server{Port: 4000, Source: model.SourceProcess, PID: 42, Name: "node"}, ":4000 node [pid 42]"},
		{"unknown name", model.Server{Port: 5000, Source: model.SourceProcess, PID: 7}, ":5000 (unknown) [pid 7]"},
		{"escape", model.Server{Port: 6000, Source: model.SourceProcess, PID: 1, Name: "\x1b[8mhidden"}, ":6000 ?[8mhidden [pid 1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Describe(tc.s); got != tc.want {
				t.Errorf("Describe = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	Table(&buf, nil, 0)
	if !strings.Contains(buf.String(), "No listening servers found.") {
		t.Errorf("empty table = %q", buf.String())
	}
}

func TestTable_HiddenHint(t *testing.T) {
	var buf bytes.Buffer
	Table(&buf, nil, 3)
	out := buf.String()
	if strings.Contains(out, "No listening servers found.") {
		t.Error("should not print the generic empty message when servers are hidden")
	}
	for _, want := range []string{"3", "confidence_threshold", "--all"} {
		if !strings.Contains(out, want) {
			t.Errorf("hidden hint missing %q in:\n%s", want, out)
		}
	}
}

func TestTable_RendersRow(t *testing.T) {
	servers := []model.Server{
		{Port: 3000, Proto: "tcp", PID: 42, Source: model.SourceProcess,
			Project: &model.Project{Name: "myapp", Description: "a cool app"}},
	}
	var buf bytes.Buffer
	Table(&buf, servers, 0)
	out := buf.String()
	for _, want := range []string{"PORT", "PROTO", "SERVER", "DESCRIPTION", "3000", "myapp", "a cool app", "proc"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q in:\n%s", want, out)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want string
	}{
		{"plain ASCII unchanged", "hello world", "hello world"},
		{"OSC title set neutralized", "\x1b]0;evil\x07", "?]0;evil?"},
		{"CSI line-erase neutralized", "\x1b[2K\r", "?[2K?"},
		{"C1 byte neutralized", "a\u009bb", "a?b"},
		{"multi-byte UTF-8 passes through", "café — ✓", "café — ✓"},
		{"newline and tab replaced", "a\nb\tc", "a?b?c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Sanitize(tc.s)
			if got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.s, got, tc.want)
			}
			for _, r := range got {
				if isUnsafeRune(r) {
					t.Errorf("Sanitize(%q) = %q still contains unsafe rune %q", tc.s, got, r)
				}
			}
		})
	}
}

func TestTable_SanitizesEscapes(t *testing.T) {
	servers := []model.Server{
		{Port: 3000, Proto: "tcp", PID: 42, Source: model.SourceProcess,
			Project: &model.Project{Name: "evil\x1b[8mname", Description: "desc\x1b]0;pwn\x07"}},
	}
	var buf bytes.Buffer
	Table(&buf, servers, 0)
	out := buf.String()
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("table output contains raw ESC:\n%q", out)
	}
}

func TestJSON_LocksFieldContract(t *testing.T) {
	servers := []model.Server{
		{Port: 3000, Proto: "tcp", PID: 42, Source: model.SourceProcess, Uptime: 90 * time.Second},
	}
	var buf bytes.Buffer
	if err := JSON(&buf, servers); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// The JSON shape is a public contract; pin the field names + the source enum.
	for _, want := range []string{`"port"`, `"proto"`, `"pid"`, `"source"`, `"uptimeNs"`, `"process"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q in:\n%s", want, out)
		}
	}
	// Human-readable uptime is added alongside uptimeNs; pin its key and value.
	// 90s → HumanUptime drops the sub-minute remainder → "1m".
	if !strings.Contains(out, `"uptime": "1m"`) {
		t.Errorf("json missing human uptime field in:\n%s", out)
	}
}
