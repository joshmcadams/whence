package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up, Down              key.Binding
	Kill, Detail, Filter  key.Binding
	All, Theme, Refresh   key.Binding
	Quit                  key.Binding

	ConfirmYes, ConfirmScope key.Binding
	ConfirmCancel            key.Binding

	DetailBack key.Binding

	FilterApply, FilterCancel key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "move")),
		Down:    key.NewBinding(key.WithKeys("down"), key.WithHelp("↑/↓", "move")),
		Kill:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "kill")),
		Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		All:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
		Theme:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "theme")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Quit:    key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q/esc", "quit")),

		ConfirmYes:    key.NewBinding(key.WithKeys("y", "yes"), key.WithHelp("y", "confirm")),
		ConfirmScope:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "toggle scope")),
		ConfirmCancel: key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel")),

		DetailBack: key.NewBinding(key.WithKeys("esc", "enter", "q"), key.WithHelp("esc/enter/q", "back")),

		FilterApply:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		FilterCancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
	}
}
