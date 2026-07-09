// Package output renders scan results as a human table or JSON.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/joshmcadams/whence/internal/model"
)

// serverJSON wraps model.Server for JSON output, adding a human-readable
// "uptime" string alongside the existing "uptimeNs" nanosecond field.
// Both fields are kept for compatibility; consumers should prefer "uptime".
type serverJSON struct {
	model.Server
	UptimeHuman string `json:"uptime"`
}

// JSON writes servers as indented JSON.
func JSON(w io.Writer, servers []model.Server) error {
	wrapped := make([]serverJSON, len(servers))
	for i, s := range servers {
		wrapped[i] = serverJSON{Server: s, UptimeHuman: HumanUptime(s.Uptime)}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(wrapped)
}

// Table writes a human-readable table. hidden is the count of servers that
// exist in the inventory but were filtered out by the confidence threshold;
// when non-zero and the table is empty, a hint is printed instead of the
// generic "nothing found" message.
func Table(w io.Writer, servers []model.Server, hidden int) {
	if len(servers) == 0 {
		if hidden > 0 {
			fmt.Fprintf(w, "No servers matched (%d listening port(s) hidden below the confidence threshold).\n", hidden)
			fmt.Fprintln(w, "Run `whence list --all` to see everything, or lower confidence_threshold.")
		} else {
			fmt.Fprintln(w, "No listening servers found.")
		}
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PORT\tPROTO\tPID\tUPTIME\tSRC\tSERVER\tDESCRIPTION")
	for _, s := range servers {
		name := Sanitize(s.DisplayName())
		if name == "" {
			name = "-"
		}
		if s.Exposure() == "all" {
			name += " [!]"
		}
		desc := Sanitize(s.Description())
		if desc == "" {
			desc = note(s)
		}
		pid := "-"
		if s.PID > 0 {
			pid = fmt.Sprint(s.PID)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Port, Sanitize(s.Proto), pid, HumanUptime(s.Uptime), SrcLabel(s.Source), name, Truncate(desc, 60))
	}
	tw.Flush()
}

func note(s model.Server) string {
	if len(s.Notes) > 0 {
		return "(" + Sanitize(s.Notes[0]) + ")"
	}
	return "-"
}

// SrcLabel is the short source tag shown in tables ("proc"/"docker").
func SrcLabel(src model.Source) string {
	switch src {
	case model.SourceDocker:
		return "docker"
	default:
		return "proc"
	}
}

// Truncate shortens s to at most n runes, adding an ellipsis when cut.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// Sanitize makes an untrusted string safe to print to a terminal: C0 control
// characters, DEL, and C1 control characters (0x80–0x9F, the range that
// encodes CSI/OSC in 8-bit form) are replaced with '?'. Newlines and tabs are
// replaced too — every render site here is single-line, and tabwriter treats
// tabs as column separators. Content is sanitized at the render boundary
// only; JSON output keeps raw values.
func Sanitize(s string) string {
	// Fast path: scan for offenders before allocating.
	clean := true
	for _, r := range s {
		if isUnsafeRune(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isUnsafeRune(r) {
			b.WriteRune('?')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isUnsafeRune(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// HumanUptime renders a duration compactly: 45s, 12m, 3h17m, 2d4h.
func HumanUptime(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, h)
	}
}
