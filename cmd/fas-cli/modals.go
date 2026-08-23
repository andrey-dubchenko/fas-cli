package main

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// indexOfString returns the index of target in items, or 0 if not found (a
// sane default for pre-selecting a dropdown option).
func indexOfString(items []string, target string) int {
	for i, s := range items {
		if s == target {
			return i
		}
	}
	return 0
}

// closeModal removes the named overlay page and returns focus to the table.
// All three modals (config/search/help) close the same way.
func (a *App) closeModal(name string) {
	a.pages.RemovePage(name)
	a.app.SetFocus(a.table)
}

// centeredModal wraps p in a fixed-size box centered within the screen,
// using nested Flexes with proportional spacer items on every side.
func centeredModal(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

// openConfigModal shows a real form for editing config.json: the default
// module/environment and each environment's disabled flag. It edits a draft
// copy so Cancel/Esc discards changes cleanly, and Save writes the draft to
// disk and adopts it as the live config.
func (a *App) openConfigModal() {
	draft := a.config
	draft.Environments = append([]Environment(nil), a.config.Environments...)

	envNames := make([]string, len(a.config.Environments))
	for i, e := range a.config.Environments {
		envNames[i] = e.Name
	}

	form := tview.NewForm()

	form.AddDropDown("Default module", a.config.Modules, indexOfString(a.config.Modules, a.config.DefaultModule),
		func(option string, _ int) {
			draft.DefaultModule = option
		})
	form.AddDropDown("Default environment", envNames, indexOfString(envNames, a.config.DefaultEnv),
		func(option string, _ int) {
			draft.DefaultEnv = option
		})
	for i, env := range a.config.Environments {
		i := i
		form.AddCheckbox(fmt.Sprintf("Disable %s", env.Name), env.Disabled, func(checked bool) {
			draft.Environments[i].Disabled = checked
		})
	}

	closeForm := func() { a.closeModal(configPageName) }

	form.AddButton("Save", func() {
		if err := saveConfig(a.configPath, draft); err != nil {
			a.err = fmt.Errorf("save config: %w", err)
			a.updateStatusView()
			return
		}
		a.config = draft
		a.refreshHeader()
		closeForm()
	})
	form.AddButton("Reload from disk", func() {
		cfg, err := loadConfig(a.configPath)
		if err != nil {
			a.err = fmt.Errorf("reload config: %w", err)
			a.updateStatusView()
		} else {
			a.config = cfg
			a.refreshHeader()
		}
		closeForm()
	})
	form.AddButton("Cancel", closeForm)
	form.SetCancelFunc(closeForm)

	form.SetBorder(true).
		SetTitle(fmt.Sprintf(" Config: %s ", a.configPath)).
		SetTitleAlign(tview.AlignLeft)

	// Form lays out each item on its own line plus one blank padding line
	// (2 dropdowns + one checkbox per environment), then the button row,
	// then the border. Undersizing this clips the later checkboxes/buttons
	// instead of showing them, so compute it exactly rather than guessing.
	numItems := 2 + len(a.config.Environments)
	height := numItems*2 + 1 /* buttons row */ + 2 /* border */ + 1 /* safety margin */
	a.pages.AddPage(configPageName, centeredModal(form, 64, height), true, true)
	a.app.SetFocus(form)
}

// openSearchModal shows a small input box that live-filters the table by
// schema name as the user types. Enter accepts the current filter and
// closes the box (leaving the list filtered); Esc clears the filter and
// closes it, the same semantics the inline search bar used to have.
func (a *App) openSearchModal() {
	input := tview.NewInputField().
		SetLabel("Search: ").
		SetText(a.searchFilter).
		SetFieldWidth(0)

	input.SetChangedFunc(func(text string) {
		a.searchFilter = text
		a.refreshTable()
	})
	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			a.searchFilter = ""
			a.refreshTable()
		}
		a.closeModal(searchPageName)
	})

	input.SetBorder(true).SetTitle(" Search by schema name ")
	a.pages.AddPage(searchPageName, centeredModal(input, 60, 3), true, true)
	a.app.SetFocus(input)
}

// helpText is the full shortcut reference shown by openHelpModal. The
// persistent bottom bar only shows a short version of this.
var helpText = []string{
	"Navigation:",
	"  ↑/↓, j/k        move cursor",
	"  PgUp/PgDn       scroll a page",
	"  Home/End        jump to top/bottom",
	"  Enter           expand/collapse a schema group",
	"  Space           toggle selection (group or version)",
	"  a               select/deselect all",
	"",
	"Switching:",
	"  F1-F9           switch environment",
	"  1-9             switch module",
	"",
	"Operations on selected versions:",
	"  s               stop",
	"  r               restart (stop, then start)",
	"  d               delete schema (and its index)",
	"  m               move alias (2 versions, same schema)",
	"",
	"Other:",
	"  f               search by schema name",
	"  c               edit config.json",
	"  ?  /  h         this help",
	"  q  /  Ctrl+C    quit",
	"",
	"Letter shortcuts also work on a Russian (ЙЦУКЕН) keyboard layout.",
	"",
	"Press Esc or Enter to close.",
}

// openHelpModal shows the full shortcut reference in a dismissible overlay.
func (a *App) openHelpModal() {
	text := tview.NewTextView().
		SetText(strings.Join(helpText, "\n")).
		SetDynamicColors(false)
	text.SetBorder(true).SetTitle(" Keyboard shortcuts ")

	text.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape, tcell.KeyEnter:
			a.closeModal(helpPageName)
			return nil
		}
		if r := event.Rune(); r == 'q' || r == '?' {
			a.closeModal(helpPageName)
			return nil
		}
		return event // let arrow keys / PgUp / PgDn scroll the text view
	})

	a.pages.AddPage(helpPageName, centeredModal(text, 70, len(helpText)+2), true, true)
	a.app.SetFocus(text)
}
