package tui

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"vanity-eth/internal/generator"
)

// uiState is the current screen of the TUI.
type uiState int

const (
	stateForm    uiState = iota // pattern entry form
	stateRunning                // search in progress
	stateResults                // search complete
)

// Internal messages.
type tickMsg time.Time
type resultMsg struct{ r generator.Result }
type doneMsg struct{}
type savedMsg struct{ path string }
type saveErrMsg struct{ err error }

// Form focus indices.
const (
	fieldPattern = 0
	fieldType    = 1
	fieldCount   = 2
	fieldWorkers = 3
	numFields    = 4
)

var patTypeNames = []string{"prefix", "suffix", "contains", "regex"}

// Model is the bubbletea application model.
type Model struct {
	state   uiState
	width   int
	height  int

	// Form fields: pattern(0), count(1), workers(2).
	// fieldType is handled separately (not a textinput).
	inputs   []textinput.Model
	focusIdx int
	patType  int // index into patTypeNames

	// Running state.
	ctx       context.Context
	cancel    context.CancelFunc
	stats     *generator.Stats
	resultCh  chan generator.Result
	startTime time.Time
	spinner   spinner.Model

	// Shared results across running/results states.
	results []generator.Result
	cfg     generator.Config

	// Status messages.
	errMsg  string
	infoMsg string

	// Final stats (captured when done).
	finalTotal   int64
	finalElapsed time.Duration
}

// New creates a fresh Model ready for the form state.
func New() Model {
	inputs := make([]textinput.Model, 3)

	inputs[0] = textinput.New()
	inputs[0].Placeholder = "dead"
	inputs[0].CharLimit = 40
	inputs[0].Width = 28
	inputs[0].Focus()

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "1"
	inputs[1].SetValue("1")
	inputs[1].CharLimit = 5
	inputs[1].Width = 8

	inputs[2] = textinput.New()
	inputs[2].Placeholder = fmt.Sprintf("%d", runtime.NumCPU())
	inputs[2].SetValue(fmt.Sprintf("%d", runtime.NumCPU()))
	inputs[2].CharLimit = 4
	inputs[2].Width = 8

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorPrimary)

	return Model{
		inputs:  inputs,
		spinner: sp,
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// ---- Update ----------------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		if m.state == stateRunning {
			return m, tick()
		}
		return m, nil

	case spinner.TickMsg:
		if m.state == stateRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case resultMsg:
		if m.state == stateRunning {
			m.results = append(m.results, msg.r)
			return m, waitForResult(m.resultCh)
		}
		return m, nil

	case doneMsg:
		m.finalTotal = m.stats.Total.Load()
		m.finalElapsed = time.Since(m.startTime)
		if m.cancel != nil {
			m.cancel()
		}
		m.state = stateResults
		return m, nil

	case savedMsg:
		m.infoMsg = "Saved to " + msg.path
		return m, nil

	case saveErrMsg:
		m.errMsg = "Save error: " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Delegate unhandled msgs to focused text input when on form.
	if m.state == stateForm {
		return m.updateFormInput(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {

	case stateForm:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Tab):
			m.focusIdx = (m.focusIdx + 1) % numFields
			m.syncFocus()
			return m, nil

		case key.Matches(msg, keys.ShiftTab):
			m.focusIdx = (m.focusIdx + numFields - 1) % numFields
			m.syncFocus()
			return m, nil

		case key.Matches(msg, keys.Left):
			if m.focusIdx == fieldType {
				m.patType = (m.patType + len(patTypeNames) - 1) % len(patTypeNames)
			}
			return m, nil

		case key.Matches(msg, keys.Right):
			if m.focusIdx == fieldType {
				m.patType = (m.patType + 1) % len(patTypeNames)
			}
			return m, nil

		case key.Matches(msg, keys.Enter):
			if err := m.prepareSearch(); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			return m, tea.Batch(
				m.runGenerator(),
				waitForResult(m.resultCh),
				tick(),
				m.spinner.Tick,
			)

		default:
			return m.updateFormInput(msg)
		}

	case stateRunning:
		switch {
		case key.Matches(msg, keys.Stop):
			if m.cancel != nil {
				m.cancel()
			}
			// doneMsg will arrive once resultCh is closed by the generator.
			return m, nil
		}

	case stateResults:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Save):
			m.infoMsg = ""
			m.errMsg = ""
			return m, saveResults(m.results)

		case key.Matches(msg, keys.New):
			next := New()
			next.width = m.width
			next.height = m.height
			return next, nil
		}
	}

	return m, nil
}

// updateFormInput delegates keyboard events to the currently focused text input.
func (m Model) updateFormInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.focusIdx == fieldType {
		return m, nil
	}
	idx := formInputIndex(m.focusIdx)
	if idx < 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.inputs[idx], cmd = m.inputs[idx].Update(msg)
	return m, cmd
}

// syncFocus blurs all inputs and focuses the active one (if applicable).
func (m *Model) syncFocus() {
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
	if m.focusIdx != fieldType {
		m.inputs[formInputIndex(m.focusIdx)].Focus()
	}
}

// formInputIndex maps a form focusIdx to an index in m.inputs.
func formInputIndex(fi int) int {
	switch fi {
	case fieldPattern:
		return 0
	case fieldCount:
		return 1
	case fieldWorkers:
		return 2
	default:
		return -1
	}
}

// prepareSearch validates form values and transitions to stateRunning.
func (m *Model) prepareSearch() error {
	pattern := strings.TrimSpace(m.inputs[0].Value())
	if pattern == "" {
		return fmt.Errorf("pattern cannot be empty")
	}
	if m.patType != 3 && !generator.IsValidHexPattern(pattern) {
		return fmt.Errorf("pattern must be hex characters (0-9, a-f)")
	}

	count, err := strconv.Atoi(strings.TrimSpace(m.inputs[1].Value()))
	if err != nil || count < 1 {
		return fmt.Errorf("count must be a positive integer")
	}

	workers, err := strconv.Atoi(strings.TrimSpace(m.inputs[2].Value()))
	if err != nil || workers < 1 {
		return fmt.Errorf("workers must be a positive integer")
	}

	m.cfg = generator.Config{Workers: workers, Count: count}
	switch m.patType {
	case 0:
		m.cfg.Prefix = pattern
	case 1:
		m.cfg.Suffix = pattern
	case 2:
		m.cfg.Contains = pattern
	case 3:
		m.cfg.Regex = pattern
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel
	m.stats = &generator.Stats{}
	m.resultCh = make(chan generator.Result, count)
	m.results = nil
	m.startTime = time.Now()
	m.errMsg = ""
	m.infoMsg = ""
	m.state = stateRunning
	return nil
}

// runGenerator fires the generator as a background tea.Cmd.
func (m Model) runGenerator() tea.Cmd {
	cfg := m.cfg
	ch := m.resultCh
	stats := m.stats
	ctx := m.ctx
	return func() tea.Msg {
		generator.Run(ctx, cfg, ch, stats)
		return nil // channel is now closed; waitForResult will deliver doneMsg
	}
}

// waitForResult blocks until the next result (or channel close).
func waitForResult(ch <-chan generator.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return resultMsg{r: r}
	}
}

// tick schedules a UI refresh every 250 ms.
func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// saveResults writes results to a timestamped file.
func saveResults(results []generator.Result) tea.Cmd {
	return func() tea.Msg {
		path := fmt.Sprintf("vanity-eth-%s.txt", time.Now().Format("20060102-150405"))
		f, err := os.Create(path)
		if err != nil {
			return saveErrMsg{err}
		}
		defer f.Close()
		for i, r := range results {
			fmt.Fprintf(f, "#%d\n", i+1)
			fmt.Fprintf(f, "Address:     %s\n", r.Address)
			fmt.Fprintf(f, "Private Key: 0x%s\n\n", r.PrivateKey)
		}
		return savedMsg{path: path}
	}
}

// ---- View ------------------------------------------------------------------

func (m Model) View() string {
	var body string
	switch m.state {
	case stateForm:
		body = m.viewForm()
	case stateRunning:
		body = m.viewRunning()
	case stateResults:
		body = m.viewResults()
	}

	box := styleBox.Render(body)

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}

// ---- Form view -------------------------------------------------------------

func (m Model) viewForm() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("vanity-eth") + "\n")
	b.WriteString(styleMuted.Render("Generate Ethereum vanity addresses") + "\n\n")

	// Pattern input
	b.WriteString(rowLabel("Pattern", m.focusIdx == fieldPattern))
	b.WriteString(m.inputs[0].View() + "\n\n")

	// Type selector
	b.WriteString(rowLabel("Type", m.focusIdx == fieldType))
	b.WriteString(renderTypeSelector(m.patType, m.focusIdx == fieldType) + "\n\n")

	// Count input
	b.WriteString(rowLabel("Count", m.focusIdx == fieldCount))
	b.WriteString(m.inputs[1].View() + "\n\n")

	// Workers input
	b.WriteString(rowLabel("Workers", m.focusIdx == fieldWorkers))
	b.WriteString(m.inputs[2].View() + "\n\n")

	// Error message
	if m.errMsg != "" {
		b.WriteString(styleDanger.Render("  "+m.errMsg) + "\n\n")
	}

	// Help line
	b.WriteString(styleHelp.Render("tab navigate  ←→ type  enter start  ctrl+c quit"))

	return b.String()
}

func rowLabel(label string, focused bool) string {
	s := styleLabel.Render(label)
	if focused {
		s = styleSelected.Render(label)
	}
	return fmt.Sprintf("%-10s  ", s)
}

func renderTypeSelector(current int, focused bool) string {
	left := styleMuted.Render("←")
	right := styleMuted.Render("→")
	name := patTypeNames[current]
	var middle string
	if focused {
		middle = styleAccent.Render(" " + name + " ")
	} else {
		middle = styleStat.Render(" " + name + " ")
	}
	return left + middle + right
}

// ---- Running view ----------------------------------------------------------

func (m Model) viewRunning() string {
	var b strings.Builder

	elapsed := time.Since(m.startTime)
	total := m.stats.Total.Load()
	found := m.stats.Found.Load()
	var rate float64
	if elapsed.Seconds() > 0 {
		rate = float64(total) / elapsed.Seconds()
	}

	b.WriteString(styleTitle.Render("vanity-eth") + "  " + m.spinner.View() + "\n")
	b.WriteString(styleMuted.Render("Searching for "+patternDesc(m.cfg)) + "\n\n")

	eta := computeETA(m.cfg, int(found), rate)
	etaStr := "—"
	if eta > 0 {
		etaStr = fmtDuration(eta)
	}

	b.WriteString(statRow("Tried", formatBig(total)) + "  " +
		statRow("Rate", fmt.Sprintf("%.0f/s", rate)) + "\n")
	b.WriteString(statRow("Found", fmt.Sprintf("%d/%d", found, m.cfg.Count)) + "  " +
		statRow("Time", fmtDuration(elapsed)) + "\n")
	b.WriteString(statRow("ETA", etaStr) + "\n\n")

	if len(m.results) > 0 {
		b.WriteString(styleSuccess.Render("Results so far:") + "\n")
		for _, r := range m.results {
			b.WriteString("  " + styleSuccess.Render("✓") + " " + styleStat.Render(truncate(r.Address, 28)) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(styleHelp.Render("ctrl+c  stop search"))
	return b.String()
}

// ---- Results view ----------------------------------------------------------

func (m Model) viewResults() string {
	var b strings.Builder

	rate := float64(m.finalTotal) / m.finalElapsed.Seconds()

	b.WriteString(styleTitle.Render("vanity-eth") + "\n")
	b.WriteString(styleSuccess.Render(fmt.Sprintf("Done! Found %d address(es)", len(m.results))) + "\n")
	b.WriteString(styleMuted.Render(fmt.Sprintf("%s tried  •  %s  •  %.0f addr/s",
		formatBig(m.finalTotal), fmtDuration(m.finalElapsed), rate)) + "\n\n")

	for i, r := range m.results {
		b.WriteString(fmt.Sprintf("%s  %s\n",
			styleMuted.Render(fmt.Sprintf("#%d", i+1)),
			styleStat.Render(r.Address)))
		b.WriteString(fmt.Sprintf("    %s  %s\n",
			styleMuted.Render("key:"),
			styleKey.Render("0x"+truncate(r.PrivateKey, 20)+"...")))
		b.WriteString("\n")
	}

	if m.infoMsg != "" {
		b.WriteString(styleSuccess.Render("✓ "+m.infoMsg) + "\n\n")
	}
	if m.errMsg != "" {
		b.WriteString(styleDanger.Render("✗ "+m.errMsg) + "\n\n")
	}

	b.WriteString(styleHelp.Render("s save  n new search  q quit"))
	return b.String()
}

// ---- Helpers ---------------------------------------------------------------

// computeETA estimates remaining time based on live rate and remaining matches.
func computeETA(cfg generator.Config, found int, ratePerSec float64) time.Duration {
	if ratePerSec <= 0 {
		return 0
	}
	d := generator.HexDifficulty(cfg.Prefix, cfg.Suffix, cfg.Contains)
	if d == nil {
		return 0 // regex patterns: difficulty unknown
	}
	remaining := cfg.Count - found
	if remaining <= 0 {
		return 0
	}
	expected := new(big.Float).SetInt(d)
	expected.Mul(expected, big.NewFloat(float64(remaining)))
	secs, _ := new(big.Float).Quo(expected, big.NewFloat(ratePerSec)).Float64()
	return time.Duration(secs * float64(time.Second))
}

func statRow(label, value string) string {
	return styleLabel.Render(label) + "  " + styleAccent.Render(value)
}

func patternDesc(cfg generator.Config) string {
	switch {
	case cfg.Prefix != "":
		return fmt.Sprintf("prefix %q", cfg.Prefix)
	case cfg.Suffix != "":
		return fmt.Sprintf("suffix %q", cfg.Suffix)
	case cfg.Contains != "":
		return fmt.Sprintf("contains %q", cfg.Contains)
	case cfg.Regex != "":
		return fmt.Sprintf("regex %q", cfg.Regex)
	default:
		return "?"
	}
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func formatBig(n int64) string {
	switch {
	case n < 1_000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	default:
		return fmt.Sprintf("%.3fB", float64(n)/1e9)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
