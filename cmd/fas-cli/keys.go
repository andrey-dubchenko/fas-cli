package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

// cyrillicShortcuts maps the Cyrillic letters that sit on the same physical
// keys as our Latin shortcut letters in the standard Russian ЙЦУКЕН layout
// (e.g. 'c' and 'с' look identical but are different runes). This lets every
// single-letter shortcut work regardless of which layout is active.
var cyrillicShortcuts = map[rune]rune{
	'й': 'q', 'ы': 's', 'в': 'd', 'к': 'r', 'ь': 'm',
	'у': 'e', 'д': 'l', 'ф': 'a', 'а': 'f', 'с': 'c', 'ч': 'x', 'р': 'h',
}

// handleKey is the single entry point for all keyboard input. It returns nil
// to swallow a key it has handled itself, or the event unchanged so the
// focused primitive's own input handler can process it: the table when no
// modal is open (arrow keys, PgUp/PgDn, Home/End, and mouse wheel scrolling
// all come for free that way), or the config/search/help modal otherwise.
func (a *App) handleKey(event *tcell.EventKey) *tcell.EventKey {
	if name, _ := a.pages.GetFrontPage(); name != mainPageName {
		// A modal owns input right now; don't steal it.
		return event
	}

	if a.busy {
		return nil
	}

	switch event.Key() {
	case tcell.KeyEscape:
		// Esc only closes modals (handled above); on the main screen it
		// must not quit the whole app by accident.
		return nil
	case tcell.KeyCtrlC:
		a.app.Stop()
		return nil
	case tcell.KeyEnter:
		a.onEnter()
		return nil
	}

	// F1-F9 switches environments.
	if event.Key() >= tcell.KeyF1 && event.Key() <= tcell.KeyF9 {
		idx := int(event.Key() - tcell.KeyF1)
		if idx < len(a.config.Environments) && a.loading == "" {
			if a.config.Environments[idx].Disabled {
				a.err = fmt.Errorf("environment %s is disabled", a.config.Environments[idx].Name)
				a.updateStatusView()
			} else {
				a.switchEnv(idx)
			}
		}
		return nil
	}

	if event.Key() != tcell.KeyRune {
		// Navigation (up/down, PgUp/PgDn, Home/End) is left to the table.
		return event
	}

	r := event.Rune()

	// 1-9 switches modules directly.
	if r >= '1' && r <= '9' {
		idx := int(r - '1')
		if idx < len(a.config.Modules) && a.loading == "" {
			a.selectModule(idx)
		}
		return nil
	}

	if latin, ok := cyrillicShortcuts[r]; ok {
		r = latin
	}

	switch r {
	case 'q':
		a.app.Stop()
	case ' ':
		a.onSpace()
	case 's':
		a.onS()
	case 'd':
		a.onD()
	case 'r':
		a.onR()
	case 'm':
		a.onM()
	case 'a':
		a.onA()
	case 'c':
		a.openConfigModal()
	case 'f':
		a.openSearchModal()
	case '?', 'h':
		a.openHelpModal()
	default:
		return event
	}
	return nil
}
