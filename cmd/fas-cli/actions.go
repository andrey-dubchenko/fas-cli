package main

import "fmt"

// loadSchemas fetches the schema list for the current environment/module on
// a goroutine and applies the result on the UI thread.
func (a *App) loadSchemas() {
	env := a.env()
	module := a.module
	endpoints := a.config.Endpoints
	go func() {
		groups, err := fetchSchemas(a.client, env.FlinkAdmin, env.OpenSearch, module, endpoints)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.err = err
				a.loading = ""
				a.table.Clear()
				a.rows = nil
				a.updateStatusView()
				return
			}

			// Every (re)load starts fully collapsed, regardless of what was
			// expanded before: a stable, predictable starting point rather
			// than carrying stale expand state across env/module switches.
			a.groups = groups
			a.err = nil
			a.loading = ""
			a.docsLoaded = false
			a.refreshTable()
			a.updateStatusView()
			a.loadDocCounts()
		})
	}()
}

// loadDocCounts annotates already-loaded groups with document counts.
func (a *App) loadDocCounts() {
	env := a.env()
	if env.OpenSearch == "" {
		// No OpenSearch for this environment: nothing to annotate, and no
		// point spinning up a goroutine to hit an empty URL.
		a.docsLoaded = true
		a.refreshTable()
		return
	}
	go func() {
		counts := fetchDocCounts(a.client, env.OpenSearch)
		a.app.QueueUpdateDraw(func() {
			a.docsLoaded = true
			if counts != nil {
				for gi := range a.groups {
					for vi := range a.groups[gi].Versions {
						v := &a.groups[gi].Versions[vi]
						if v.IndexName != "" {
							if dc, ok := counts[v.IndexName]; ok {
								v.DocCount = dc
							}
						}
					}
				}
			}
			a.refreshTable()
		})
	}()
}

// switchEnv resets state and reloads data for the new environment.
func (a *App) switchEnv(idx int) {
	if idx == a.envIdx {
		return
	}
	a.envIdx = idx
	a.groups = nil
	a.rows = nil
	a.table.Clear()
	a.opResult = nil
	a.err = nil
	a.docsLoaded = false
	a.loading = fmt.Sprintf("Loading schemas [%s]...", a.env().Name)
	a.refreshHeader()
	a.updateStatusView()
	a.loadSchemas()
}

// runOps executes opType against items one at a time on a goroutine,
// appending each step's log lines as they complete so the user sees live
// progress, then reloads the schema list once the queue is drained.
func (a *App) runOps(opType string, items []opItem) {
	if len(items) == 0 {
		return
	}

	busyMsgs := map[string]string{
		"stop":    "Stopping selected schemas...",
		"delete":  "Deleting selected schemas and indices...",
		"restart": "Restarting selected schemas...",
	}
	a.busy = true
	a.busyMsg = busyMsgs[opType]
	a.opResult = nil
	a.updateStatusView()

	env := a.env()
	endpoints := a.config.Endpoints
	go func() {
		for _, item := range items {
			cur := item
			for {
				lines, next := processNextOp(a.client, env.FlinkAdmin, env.OpenSearch, opType, cur, endpoints)
				a.app.QueueUpdateDraw(func() {
					a.opResult = append(a.opResult, lines...)
					a.updateStatusView()
				})
				if next == nil {
					break
				}
				cur = *next
			}
		}
		a.app.QueueUpdateDraw(func() {
			a.busy = false
			a.busyMsg = ""
			a.loading = "Reloading schemas..."
			a.updateStatusView()
			a.loadSchemas()
		})
	}()
}

// runMoveAlias moves aliases between two indices on a goroutine.
func (a *App) runMoveAlias(opensearchURL, src, dst string, aliases []string) {
	a.busy = true
	a.busyMsg = fmt.Sprintf("Moving aliases from %s to %s...", src, dst)
	a.opResult = nil
	a.updateStatusView()

	go func() {
		lines := moveAlias(a.client, opensearchURL, src, dst, aliases)
		a.app.QueueUpdateDraw(func() {
			a.opResult = append(a.opResult, lines...)
			a.busy = false
			a.busyMsg = ""
			a.loading = "Reloading schemas..."
			a.updateStatusView()
			a.loadSchemas()
		})
	}()
}

func (a *App) validateMoveAlias() (groupIdx int, srcIdx int, dstIdx int, errMsg string) {
	selected := make(map[int][]int)
	for gi, g := range a.groups {
		for vi, v := range g.Versions {
			if v.Selected {
				selected[gi] = append(selected[gi], vi)
			}
		}
	}

	if len(selected) == 0 {
		return 0, 0, 0, "Nothing selected"
	}
	if len(selected) > 1 {
		return 0, 0, 0, "Select exactly 2 versions from the same schema"
	}

	// Exactly one entry at this point (checked above), so this loop just
	// reads the map's only key/value pair without needing its key upfront.
	var gi int
	var vis []int
	for gi, vis = range selected {
	}

	if len(vis) != 2 {
		return 0, 0, 0, "Select exactly 2 versions for alias move"
	}

	v0 := a.groups[gi].Versions[vis[0]]
	v1 := a.groups[gi].Versions[vis[1]]

	if v0.IndexName == "" || v1.IndexName == "" {
		return 0, 0, 0, "Both versions must have an index"
	}

	has0 := len(v0.Aliases) > 0
	has1 := len(v1.Aliases) > 0

	if has0 && has1 {
		return 0, 0, 0, "Both versions have aliases — cannot determine direction"
	}
	if !has0 && !has1 {
		return 0, 0, 0, "Neither version has an alias to move"
	}

	if has0 {
		return gi, vis[0], vis[1], ""
	}
	return gi, vis[1], vis[0], ""
}

// selectedRow returns the row currently highlighted in the table, or false
// if there is no valid selection (e.g. the table is empty).
func (a *App) selectedRow() (rowRef, bool) {
	row, _ := a.table.GetSelection()
	if row < 0 || row >= len(a.rows) {
		return rowRef{}, false
	}
	return a.rows[row], true
}

func (a *App) onEnter() {
	r, ok := a.selectedRow()
	if !ok {
		return
	}
	if r.versionIdx == -1 {
		a.groups[r.groupIdx].Expanded = !a.groups[r.groupIdx].Expanded
		a.refreshTable()
	}
}

func (a *App) onSpace() {
	r, ok := a.selectedRow()
	if !ok {
		return
	}
	if r.versionIdx >= 0 {
		v := &a.groups[r.groupIdx].Versions[r.versionIdx]
		v.Selected = !v.Selected
	} else {
		allSelected := true
		for _, v := range a.groups[r.groupIdx].Versions {
			if !v.Selected {
				allSelected = false
				break
			}
		}
		for i := range a.groups[r.groupIdx].Versions {
			a.groups[r.groupIdx].Versions[i].Selected = !allSelected
		}
	}
	a.refreshTable()
}

func (a *App) onS() {
	a.runOps("stop", collectSelected(a.groups))
}

func (a *App) onD() {
	a.runOps("delete", collectSelected(a.groups))
}

func (a *App) onR() {
	a.runOps("restart", collectSelected(a.groups))
}

func (a *App) selectModule(idx int) {
	if idx >= len(a.config.Modules) {
		return
	}
	old := a.module
	a.module = a.config.Modules[idx]
	a.refreshHeader()
	if old != a.module {
		a.loading = fmt.Sprintf("Loading schemas [%s]...", a.env().Name)
		a.updateStatusView()
		a.loadSchemas()
	}
}

func (a *App) onM() {
	if a.env().OpenSearch == "" {
		a.opResult = []string{"[red::B]  Move alias is unavailable: no OpenSearch configured for this environment[-:-:-]"}
		a.updateStatusView()
		return
	}
	gi, srcIdx, dstIdx, errMsg := a.validateMoveAlias()
	if errMsg != "" {
		a.opResult = []string{"[red::B]  " + errMsg + "[-:-:-]"}
		a.updateStatusView()
		return
	}
	src := a.groups[gi].Versions[srcIdx]
	dst := a.groups[gi].Versions[dstIdx]
	a.runMoveAlias(a.env().OpenSearch, src.IndexName, dst.IndexName, src.Aliases)
}

func (a *App) onA() {
	anySelected := false
outer:
	for _, g := range a.groups {
		for _, v := range g.Versions {
			if v.Selected {
				anySelected = true
				break outer
			}
		}
	}
	for gi := range a.groups {
		for vi := range a.groups[gi].Versions {
			a.groups[gi].Versions[vi].Selected = !anySelected
		}
	}
	a.refreshTable()
}
