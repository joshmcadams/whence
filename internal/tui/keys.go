package tui

import bkey "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down             bkey.Binding
	Kill, Detail, Filter bkey.Binding
	All, Theme, Refresh  bkey.Binding
	Sort                 bkey.Binding
	Quit                 bkey.Binding

	ConfirmYes, ConfirmScope bkey.Binding
	ConfirmCancel            bkey.Binding

	DetailBack bkey.Binding

	FilterApply, FilterCancel bkey.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:      bkey.NewBinding(bkey.WithKeys("up"), bkey.WithHelp("↑/↓", "move")),
		Down:    bkey.NewBinding(bkey.WithKeys("down"), bkey.WithHelp("↑/↓", "move")),
		Kill:    bkey.NewBinding(bkey.WithKeys("x"), bkey.WithHelp("x", "kill")),
		Detail:  bkey.NewBinding(bkey.WithKeys("enter"), bkey.WithHelp("enter", "details")),
		Filter:  bkey.NewBinding(bkey.WithKeys("/"), bkey.WithHelp("/", "filter")),
		All:     bkey.NewBinding(bkey.WithKeys("a"), bkey.WithHelp("a", "all")),
		Theme:   bkey.NewBinding(bkey.WithKeys("t"), bkey.WithHelp("t", "theme")),
		Sort:    bkey.NewBinding(bkey.WithKeys("s"), bkey.WithHelp("s", "sort")),
		Refresh: bkey.NewBinding(bkey.WithKeys("r"), bkey.WithHelp("r", "refresh")),
		Quit:    bkey.NewBinding(bkey.WithKeys("q", "esc"), bkey.WithHelp("q/esc", "quit")),

		ConfirmYes:    bkey.NewBinding(bkey.WithKeys("y", "yes"), bkey.WithHelp("y", "confirm")),
		ConfirmScope:  bkey.NewBinding(bkey.WithKeys("s"), bkey.WithHelp("s", "toggle scope")),
		ConfirmCancel: bkey.NewBinding(bkey.WithKeys("n", "esc"), bkey.WithHelp("n/esc", "cancel")),

		DetailBack: bkey.NewBinding(bkey.WithKeys("esc", "enter", "q"), bkey.WithHelp("esc/enter/q", "back")),

		FilterApply:  bkey.NewBinding(bkey.WithKeys("enter"), bkey.WithHelp("enter", "apply")),
		FilterCancel: bkey.NewBinding(bkey.WithKeys("esc"), bkey.WithHelp("esc", "clear")),
	}
}
