package ui

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"fas-cli/internal/config"
	"fas-cli/internal/flink"
)

// Table cell colors, named via tcell's W3C color set. Colors used only in
// TextViews (header, errors, status) are inline tag strings instead, since
// tview tags cannot reference a Go color variable.
var (
	colorStatus  = tcell.ColorYellow
	colorGroup   = tcell.ColorAqua
	colorAlias   = tcell.ColorFuchsia
	colorNoIndex = tcell.ColorGray
	colorIndex   = tcell.ColorTeal
	colorDefault = tcell.ColorWhite
)

// Overlay page names used with App.pages.
const (
	mainPageName   = "main"
	configPageName = "config"
	searchPageName = "search"
	helpPageName   = "help"

	confirmPageName = "confirm"
)

// rowRef identifies one visible line in the table: either a schema group
// header (versionIdx == -1) or one version belonging to a group.
type rowRef struct {
	groupIdx   int
	versionIdx int
}

// App holds all UI state and wires the tview widgets together.
type App struct {
	app *tview.Application

	pages      *tview.Pages
	envTabs    *tview.TextView
	titleView  *tview.TextView
	table      *tview.Table
	statusView *tview.TextView
	helpView   *tview.TextView
	flex       *tview.Flex

	config     config.Config
	configPath string
	envIdx     int
	module     string
	client     *http.Client

	groups     []flink.SchemaGroup
	rows       []rowRef
	docsLoaded bool
	loadGen    int // bumped on every loadSchemas; stale async results are dropped

	err     error
	loading string

	busy     bool
	busyMsg  string
	opResult []string

	searchFilter string
}

// env returns the currently selected environment, or a zero-value
// Environment if none are configured yet (empty flinkAdmin/opensearch URLs,
// which every network-calling function already treats as "nothing to do").
func (a *App) env() config.Environment {
	if a.envIdx < 0 || a.envIdx >= len(a.config.Environments) {
		return config.Environment{}
	}
	return a.config.Environments[a.envIdx]
}

// NewApp builds the tview widget tree and wires up input handling. The
// layout is a single vertical Flex: fixed env-tabs and title bars on top,
// the schema table taking whatever space is left (proportion 1, so it never
// needs manual height math), a variable-height status/results strip, and a
// fixed help bar at the bottom.
func NewApp(module string, cfg config.Config, envIdx int, cfgPath string) *App {
	// The timeout keeps a hung server from freezing the UI in the busy state.
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
		},
	}

	a := &App{
		app:        tview.NewApplication(),
		config:     cfg,
		configPath: cfgPath,
		envIdx:     envIdx,
		module:     module,
		client:     client,
	}

	a.envTabs = tview.NewTextView().SetDynamicColors(true)
	a.titleView = tview.NewTextView().SetDynamicColors(true)
	a.table = tview.NewTable().SetSelectable(true, false)
	// Without an explicit selected style, tview highlights a selected row by
	// inverting each cell's own text color independently, which turns our
	// multi-colored rows (green checkbox, yellow status, teal index, ...)
	// into a patchwork of different background colors per column instead of
	// one solid highlight bar. A single style makes the whole row uniform.
	a.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorBlue).Foreground(tcell.ColorWhite))
	// Column 0 holds a 1-char group arrow on header rows but a 5-char
	// checkbox on version rows. By default tview sizes each column from only
	// the currently visible rows, so depending on scroll position it could
	// size column 0 from an arrow row and clip every checkbox on screen down
	// to their leading blank space. Evaluating all rows keeps it consistent.
	a.table.SetEvaluateAllRows(true)
	a.statusView = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	a.helpView = tview.NewTextView().SetDynamicColors(true)

	a.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.envTabs, 1, 0, false).
		AddItem(a.titleView, 1, 0, false).
		AddItem(a.table, 0, 1, true).
		AddItem(a.statusView, 0, 0, false).
		AddItem(a.helpView, 1, 0, false)

	a.pages = tview.NewPages().AddPage(mainPageName, a.flex, true, true)

	a.app.SetRoot(a.pages, true).SetFocus(a.table).EnableMouse(true)
	a.app.SetInputCapture(a.handleKey)

	a.refreshHeader()
	a.updateHelp()

	return a
}

// Run starts the initial load and blocks until the application exits.
func (a *App) Run() error {
	if len(a.config.Environments) == 0 {
		// Nothing configured yet: say so plainly instead of trying to fetch
		// schemas from an empty Flink Admin URL. Add an environment to
		// config.json, then 'c' -> "Reload from disk" to pick it up.
		a.updateStatusView()
	} else {
		a.loading = fmt.Sprintf("Loading schemas [%s]...", a.env().Name)
		a.updateStatusView()
		a.loadSchemas()
	}
	return a.app.Run()
}
