package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// Theme is a named color palette for the TUI. The selected-row colors are the
// most important for readability; header/accent tint the chrome.
type Theme struct {
	Name string
	// Reverse uses terminal reverse-video for the selected row (always legible,
	// regardless of the terminal's color scheme). When true, Selected* are unused.
	Reverse    bool
	SelectedFg string
	SelectedBg string
	Header     string
	Accent     string
}

// themes are the available palettes, in cycle order. All selected-row pairings
// are deliberately high-contrast (dark text on a bright fill, or light text on a
// deep fill) to fix the unreadable black-on-bright-blue default.
var themes = []Theme{
	{Name: "indigo", SelectedFg: "231", SelectedBg: "61", Header: "111", Accent: "111"},
	{Name: "teal", SelectedFg: "232", SelectedBg: "43", Header: "44", Accent: "44"},
	{Name: "amber", SelectedFg: "232", SelectedBg: "214", Header: "215", Accent: "214"},
	{Name: "magenta", SelectedFg: "231", SelectedBg: "127", Header: "176", Accent: "176"},
	{Name: "green", SelectedFg: "232", SelectedBg: "78", Header: "114", Accent: "114"},
	{Name: "mono", Reverse: true, Header: "250", Accent: "252"},
}

// ThemeByName returns the named theme, or the default (first) theme if unknown.
func ThemeByName(name string) Theme {
	for _, t := range themes {
		if t.Name == name {
			return t
		}
	}
	return themes[0]
}

// nextTheme returns the theme after the named one, wrapping around.
func nextTheme(name string) Theme {
	for i, t := range themes {
		if t.Name == name {
			return themes[(i+1)%len(themes)]
		}
	}
	return themes[0]
}

// tableStyles builds bubbles/table styles for this theme.
func (t Theme) tableStyles() table.Styles {
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true).BorderBottom(true).Foreground(lipgloss.Color(t.Header))
	sel := st.Selected.Bold(true)
	if t.Reverse {
		sel = sel.Reverse(true)
	} else {
		sel = sel.Foreground(lipgloss.Color(t.SelectedFg)).Background(lipgloss.Color(t.SelectedBg))
	}
	st.Selected = sel
	return st
}

// accentStyle is the lipgloss style for titles/highlights in this theme.
func (t Theme) accentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Accent))
}
