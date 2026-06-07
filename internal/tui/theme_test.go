package tui

import "testing"

func TestThemeByName_FallsBackToDefault(t *testing.T) {
	if got := ThemeByName("does-not-exist"); got.Name != themes[0].Name {
		t.Errorf("unknown theme = %q, want default %q", got.Name, themes[0].Name)
	}
	if got := ThemeByName("amber"); got.Name != "amber" {
		t.Errorf("known theme = %q, want amber", got.Name)
	}
}

func TestNextTheme_Wraps(t *testing.T) {
	last := themes[len(themes)-1].Name
	if got := nextTheme(last); got.Name != themes[0].Name {
		t.Errorf("nextTheme(%q) = %q, want wrap to %q", last, got.Name, themes[0].Name)
	}
	if got := nextTheme(themes[0].Name); got.Name != themes[1].Name {
		t.Errorf("nextTheme(%q) = %q, want %q", themes[0].Name, got.Name, themes[1].Name)
	}
}

func TestTableStylesBuild(t *testing.T) {
	// Both color-based and reverse themes must produce styles without panicking.
	for _, th := range themes {
		_ = th.tableStyles()
		_ = th.accentStyle()
	}
}
