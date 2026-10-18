package ui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
)

// refreshHeader repaints the environment tabs and the title bar. Called
// whenever the environment list, active environment, or module changes.
func (a *App) refreshHeader() {
	var tabs []string
	for i, env := range a.config.Environments {
		isProd := strings.ToUpper(env.Name) == "PROD"
		label := fmt.Sprintf("F%d:%s", i+1, tview.Escape(env.Name))
		switch {
		case env.Disabled:
			// Plain foreground color rather than a text attribute: tcell's
			// native Windows console backend does not implement strikethrough
			// (or dim) at all, so color is the one thing guaranteed to render.
			tabs = append(tabs, fmt.Sprintf("[red] %s [disabled] [-:-:-]", label))
		case i == a.envIdx && isProd:
			tabs = append(tabs, fmt.Sprintf("[white:red:B] %s [-:-:-]", label))
		case i == a.envIdx:
			tabs = append(tabs, fmt.Sprintf("[black:blue:B] %s [-:-:-]", label))
		case isProd:
			tabs = append(tabs, fmt.Sprintf("[red::B] %s [-:-:-]", label))
		default:
			tabs = append(tabs, fmt.Sprintf("[gray] %s [-:-:-]", label))
		}
	}
	a.envTabs.SetText(strings.Join(tabs, " "))

	moduleLabel := "all modules"
	if a.module != "" {
		moduleLabel = "module: " + a.module
	}
	a.titleView.SetText(fmt.Sprintf("[blue::B]Flink Schemas — %s[-:-:-]", tview.Escape(moduleLabel)))
}

// buildRows flattens the current groups (respecting Expanded and the active
// search filter) into the rows the table displays.
func (a *App) buildRows() []rowRef {
	var rows []rowRef
	filter := strings.ToLower(a.searchFilter)
	for gi, g := range a.groups {
		if filter != "" && !strings.Contains(strings.ToLower(g.Name), filter) {
			continue
		}
		rows = append(rows, rowRef{groupIdx: gi, versionIdx: -1})
		if g.Expanded {
			for vi := range g.Versions {
				rows = append(rows, rowRef{groupIdx: gi, versionIdx: vi})
			}
		}
	}
	return rows
}

// refreshTable rebuilds the table's rows from the current groups/filter and
// tries to keep the selection on the same logical row (falling back to the
// row's group header, then to the previous numeric row).
func (a *App) refreshTable() {
	prevRow, _ := a.table.GetSelection()
	var prevRef *rowRef
	if prevRow >= 0 && prevRow < len(a.rows) {
		r := a.rows[prevRow]
		prevRef = &r
	}

	a.rows = a.buildRows()
	a.table.Clear()
	for i, r := range a.rows {
		a.setRowCells(i, r)
	}

	newRow := -1
	if prevRef != nil {
		for i, r := range a.rows {
			if r.groupIdx == prevRef.groupIdx && r.versionIdx == prevRef.versionIdx {
				newRow = i
				break
			}
		}
		if newRow < 0 {
			for i, r := range a.rows {
				if r.groupIdx == prevRef.groupIdx && r.versionIdx == -1 {
					newRow = i
					break
				}
			}
		}
	}
	if newRow < 0 {
		newRow = prevRow
	}
	if newRow >= len(a.rows) {
		newRow = len(a.rows) - 1
	}
	if newRow < 0 {
		newRow = 0
	}
	if len(a.rows) > 0 {
		a.table.Select(newRow, 0)
	}

	a.updateHelp()
}

func (a *App) setRowCells(i int, r rowRef) {
	g := a.groups[r.groupIdx]

	if r.versionIdx == -1 {
		// Padded to the same rune count as the "  ( )"/"  (X)" checkbox text
		// below, so column 0 never needs to change width between a group
		// header row and a version row.
		arrow := "  ▸  "
		if g.Expanded {
			arrow = "  ▾  "
		}
		sel := 0
		for _, v := range g.Versions {
			if v.Selected {
				sel++
			}
		}
		label := g.Name
		if len(g.Versions) > 1 {
			label += fmt.Sprintf(" (%d versions)", len(g.Versions))
		}
		if sel > 0 {
			label += fmt.Sprintf(" [%d selected]", sel)
		}
		a.table.SetCell(i, 0, tview.NewTableCell(arrow).SetTextColor(colorGroup))
		a.table.SetCell(i, 1, tview.NewTableCell(label).SetTextColor(colorGroup).SetExpansion(1))
		for c := 2; c <= 5; c++ {
			a.table.SetCell(i, c, tview.NewTableCell(""))
		}
		return
	}

	v := g.Versions[r.versionIdx]
	// Deliberately "( )"/"(X)", not "[ ]"/"[X]": Table sizes columns using
	// TaggedStringWidth, which treats any "[...]" as a (zero-width) color
	// tag even though Table's own drawing never renders tags. A selected
	// "[X]" cell would get measured as near-zero width and clipped down to
	// its two leading spaces, hiding the mark entirely.
	check := "  ( )"
	checkColor := colorDefault
	if v.Selected {
		check = "  (X)"
		checkColor = colorStatus // yellow: already proven visible in this UI
	}
	a.table.SetCell(i, 0, tview.NewTableCell(check).SetTextColor(checkColor))
	a.table.SetCell(i, 1, tview.NewTableCell("  "+v.Version).SetTextColor(colorDefault))
	a.table.SetCell(i, 2, tview.NewTableCell(v.Status).SetTextColor(colorStatus))

	indexText, indexColor := "(no index)", colorNoIndex
	aliasText, aliasColor := "", colorNoIndex
	docsText, docsColor := "", colorNoIndex
	if v.IndexName != "" {
		indexText, indexColor = v.IndexName, colorIndex
		if len(v.Aliases) > 0 {
			aliasText, aliasColor = strings.Join(v.Aliases, ", "), colorAlias
		} else {
			aliasText = "-"
		}
		switch {
		case !a.docsLoaded:
			docsText = "..."
		case v.DocCount == "":
			docsText = "?" // count fetch failed or index missing from the response
		case v.DocCount == "-":
			docsText = "-"
		default:
			docsText, docsColor = v.DocCount+" docs", colorStatus
		}
	}
	a.table.SetCell(i, 3, tview.NewTableCell(indexText).SetTextColor(indexColor))
	a.table.SetCell(i, 4, tview.NewTableCell(aliasText).SetTextColor(aliasColor))
	a.table.SetCell(i, 5, tview.NewTableCell(docsText).SetTextColor(docsColor))
}

// updateStatusView repaints the strip between the table and the help bar
// (loading/error messages, the busy indicator, and operation results) and
// resizes it in the Flex to fit its content, capped so it can never push the
// table off screen.
func (a *App) updateStatusView() {
	var lines []string
	switch {
	case len(a.config.Environments) == 0:
		lines = append(lines, "[yellow::B]No environments configured.[-:-:-]")
		lines = append(lines, "Add one to config.json, then press 'c' and \"Reload from disk\".")
	case a.err != nil:
		lines = append(lines, fmt.Sprintf("[red::B]Error: %s[-:-:-]", tview.Escape(a.err.Error())))
		lines = append(lines, "[gray]Press q to quit, F1-F9 to switch env, 1-9 to switch module[-:-:-]")
		// Keep operation results visible even when the follow-up reload failed.
		if len(a.opResult) > 0 {
			lines = append(lines, "[blue::B]Results:[-:-:-]")
			lines = append(lines, a.opResult...)
		}
	case a.loading != "":
		lines = append(lines, tview.Escape(a.loading))
	default:
		if len(a.groups) == 0 {
			lines = append(lines, "No schemas found.")
		}
		if a.busy {
			lines = append(lines, fmt.Sprintf("[blue::B]%s[-:-:-]", tview.Escape(a.busyMsg)))
		}
		if len(a.opResult) > 0 {
			lines = append(lines, "[blue::B]Results:[-:-:-]")
			lines = append(lines, a.opResult...)
		}
	}

	// Over the cap, keep the newest lines (latest progress and failures) and
	// replace the oldest with a marker.
	const maxHeight = 15
	if len(lines) > maxHeight {
		skipped := len(lines) - maxHeight + 1
		lines = append([]string{fmt.Sprintf("[gray]... %d earlier lines hidden[-:-:-]", skipped)}, lines[skipped:]...)
	}
	height := len(lines)
	a.statusView.SetText(strings.Join(lines, "\n"))
	a.flex.ResizeItem(a.statusView, height, 0)
}

// updateHelp repaints the bottom help bar. It stays short on purpose: the
// full shortcut reference lives behind '?' (openHelpModal) so this line
// never wraps and gets clipped by its single-row Flex slot.
func (a *App) updateHelp() {
	selectedCount := 0
	for _, g := range a.groups {
		for _, v := range g.Versions {
			if v.Selected {
				selectedCount++
			}
		}
	}

	help := []string{"↑↓ nav", "Enter/Space", "F1-F9 env"}
	if len(a.config.Modules) > 0 {
		help = append(help, "1-9 module")
	}
	help = append(help, "f search", "c config", "? help", "q quit")
	if selectedCount > 0 {
		help = append(help, fmt.Sprintf("s/r/d (%d)", selectedCount))
		if selectedCount == 2 && a.env().OpenSearch != "" {
			help = append(help, "m move")
		}
	}
	a.helpView.SetText("[gray]" + strings.Join(help, " | ") + "[-:-:-]")
}
