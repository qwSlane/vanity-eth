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
	fieldPrefix   = 0
	fieldSuffix   = 1
	fieldContains = 2
	fieldCount    = 3
	fieldWorkers  = 4
	fieldCase     = 5
	numFields     = 6
)

// inputIndex maps a focusIdx to m.inputs slice index (-1 if not a text input).
func inputIndex(fi int) int {
	switch fi {
	case fieldPrefix:
		return 0
	case fieldSuffix:
		return 1
	case fieldContains:
		return 2
	case fieldCount:
		return 3
	case fieldWorkers:
		return 4
	default:
		return -1
	}
}

// Model is the bubbletea application model.
type Model struct {
	state  uiState
	width  int
	height int

	// Form: prefix(0) suffix(1) contains(2) count(3) workers(4).
	inputs        []textinput.Model
	focusIdx      int
	caseSensitive bool

	// Running state.
	ctx       context.Context
	cancel    context.CancelFunc
	stats     *generator.Stats
	resultCh  chan generator.Result
	startTime time.Time
	spinner   spinner.Model

	// Shared.
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
	inputs := make([]textinput.Model, 5)

	newInput := func(placeholder string, width int) textinput.Model {
		t := textinput.New()
		t.Placeholder = placeholder
		t.CharLimit = 40
		t.Width = width
		return t
	}

	inputs[0] = newInput("e.g. e|f|ff", 28) // prefix
	inputs[1] = newInput("e.g. e|f|ff", 28) // suffix
	inputs[2] = newInput("e.g. e|f|ff", 28) // contains
	inputs[3] = newInput("1", 6)            // count
	inputs[3].SetValue("1")
	inputs[4] = newInput(fmt.Sprintf("%d", runtime.NumCPU()), 6) // workers
	inputs[4].SetValue(fmt.Sprintf("%d", runtime.NumCPU()))

	inputs[0].Focus()

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
		return m.updateActiveInput(msg)
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

		case key.Matches(msg, keys.Down, keys.Right):
			m.focusIdx = (m.focusIdx + 1) % numFields
			m.syncFocus()
			return m, nil

		case key.Matches(msg, keys.ShiftTab):
			m.focusIdx = (m.focusIdx + numFields - 1) % numFields
			m.syncFocus()
			return m, nil

		case key.Matches(msg, keys.Up, keys.Left):
			m.focusIdx = (m.focusIdx + numFields - 1) % numFields
			m.syncFocus()
			return m, nil

		case msg.String() == " " && m.focusIdx == fieldCase:
			m.caseSensitive = !m.caseSensitive
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
			return m.updateActiveInput(msg)
		}

	case stateRunning:
		if key.Matches(msg, keys.Stop) {
			if m.cancel != nil {
				m.cancel()
			}
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

// updateActiveInput forwards the message to the focused text input and
// validates hex fields in real time.
func (m Model) updateActiveInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	idx := inputIndex(m.focusIdx)
	if idx < 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.inputs[idx], cmd = m.inputs[idx].Update(msg)

	// Real-time hex validation for prefix/suffix/contains.
	if m.focusIdx == fieldPrefix || m.focusIdx == fieldSuffix || m.focusIdx == fieldContains {
		m.errMsg = hexValidationError(m.inputs[idx].Value(), fieldLabel(m.focusIdx))
	}
	return m, cmd
}

func fieldLabel(fi int) string {
	switch fi {
	case fieldPrefix:
		return "prefix"
	case fieldSuffix:
		return "suffix"
	case fieldContains:
		return "contains"
	default:
		return ""
	}
}

// hexValidationError returns an error string if val contains invalid chars.
// Allows alternation and grouped patterns, e.g. "dead|cafe" or "(0|e|f)(00|ff)".
func hexValidationError(val, label string) string {
	if strings.TrimSpace(val) == "" {
		return ""
	}
	if err := generator.ValidateHexPattern(val); err != nil {
		return fmt.Sprintf("%s: %v", label, err)
	}
	return ""
}

// syncFocus blurs all inputs and focuses the active one (if applicable).
func (m *Model) syncFocus() {
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
	if idx := inputIndex(m.focusIdx); idx >= 0 {
		m.inputs[idx].Focus()
	}
}

// prepareSearch validates form values and transitions to stateRunning.
func (m *Model) prepareSearch() error {
	prefix := strings.TrimSpace(m.inputs[0].Value())
	suffix := strings.TrimSpace(m.inputs[1].Value())
	contains := strings.TrimSpace(m.inputs[2].Value())

	if prefix == "" && suffix == "" && contains == "" {
		return fmt.Errorf("enter at least one of: prefix, suffix, or contains")
	}
	for label, val := range map[string]string{"prefix": prefix, "suffix": suffix, "contains": contains} {
		if val != "" {
			if err := generator.ValidateHexPattern(val); err != nil {
				return fmt.Errorf("%s: %v", label, err)
			}
		}
	}

	count, err := strconv.Atoi(strings.TrimSpace(m.inputs[3].Value()))
	if err != nil || count < 1 {
		return fmt.Errorf("count must be a positive integer")
	}

	workers, err := strconv.Atoi(strings.TrimSpace(m.inputs[4].Value()))
	if err != nil || workers < 1 {
		return fmt.Errorf("workers must be a positive integer")
	}

	m.cfg = generator.Config{
		Prefix:        prefix,
		Suffix:        suffix,
		Contains:      contains,
		Workers:       workers,
		Count:         count,
		CaseSensitive: m.caseSensitive,
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
		return nil
	}
}

func waitForResult(ch <-chan generator.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return resultMsg{r: r}
	}
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

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
		return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Center, box)
	}
	return box
}

// ---- Form view -------------------------------------------------------------

func (m Model) viewForm() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("vanity-eth") + "\n")
	b.WriteString(styleMuted.Render("Generate Ethereum vanity addresses") + "\n\n")

	row := func(label string, fi int, field string) string {
		lbl := styleLabel
		if m.focusIdx == fi {
			lbl = styleSelected
		}
		return lbl.Width(11).Render(label) + "  " + field + "\n"
	}

	b.WriteString(row("Prefix", fieldPrefix, m.inputs[0].View()))
	b.WriteString(row("Suffix", fieldSuffix, m.inputs[1].View()))
	b.WriteString(row("Contains", fieldContains, m.inputs[2].View()))
	b.WriteString("\n")
	b.WriteString(row("Count", fieldCount, m.inputs[3].View()))
	b.WriteString(row("Workers", fieldWorkers, m.inputs[4].View()))

	// Case-sensitive toggle
	box := "[ ]"
	if m.caseSensitive {
		box = styleSuccess.Render("[✓]")
	}
	caseLbl := styleLabel
	if m.focusIdx == fieldCase {
		caseLbl = styleSelected
	}
	b.WriteString(caseLbl.Width(11).Render("Case") + "  " + box + " sensitive\n")

	b.WriteString("\n")

	// Live preview
	b.WriteString(renderPreview(
		m.inputs[0].Value(),
		m.inputs[1].Value(),
		m.inputs[2].Value(),
	))

	// Difficulty hint
	if d := generator.HexDifficulty(
		m.inputs[0].Value(),
		m.inputs[1].Value(),
		m.inputs[2].Value(),
		m.caseSensitive,
	); d != nil {
		b.WriteString(styleMuted.Render("  ~1 in " + formatBigInt(d) + "\n"))
	}

	b.WriteString("\n")

	if m.errMsg != "" {
		b.WriteString(styleDanger.Render("  "+m.errMsg) + "\n\n")
	}

	help := styleHelp.PaddingLeft(12)
	b.WriteString(help.Render("up/down/tab move between fields") + "\n")
	b.WriteString(help.Render("space toggles case sensitive") + "\n")
	b.WriteString(help.Render("enter starts search") + "\n")
	b.WriteString(help.Render("esc/ctrl+c/q quits"))
	return b.String()
}

// renderPreview builds a colour-coded address skeleton.
// Patterns with | alternation (e.g. "e|f|ff") are shown as "(e|f|ff)".
func renderPreview(prefix, suffix, contains string) string {
	const addrLen = 40
	prefix = strings.ToLower(prefix)
	suffix = strings.ToLower(suffix)
	contains = strings.ToLower(contains)

	// patToken returns the display text and hex positions consumed (min alt length).
	patToken := func(pat string) (string, int) {
		if pat == "" {
			return "", 0
		}
		minLen := generator.MinHexPatternLen(pat)
		if strings.Contains(pat, "|") && !strings.HasPrefix(pat, "(") {
			return "(" + pat + ")", minLen
		}
		return pat, minLen
	}

	prefixTok, prefixLen := patToken(prefix)
	suffixTok, suffixLen := patToken(suffix)
	containsTok, containsLen := patToken(contains)

	var b strings.Builder
	b.WriteString(styleMuted.Render("  Preview") + "  0x")

	if prefixTok != "" {
		b.WriteString(styleSuccess.Render(prefixTok))
	}

	middle := addrLen - prefixLen - suffixLen
	if containsTok != "" && containsLen <= middle {
		before := (middle - containsLen) / 2
		after := middle - before - containsLen
		for i := 0; i < before; i++ {
			b.WriteString(styleMuted.Render("?"))
		}
		b.WriteString(styleAccent.Render(containsTok))
		for i := 0; i < after; i++ {
			b.WriteString(styleMuted.Render("?"))
		}
	} else {
		for i := 0; i < middle; i++ {
			b.WriteString(styleMuted.Render("?"))
		}
	}

	if suffixTok != "" {
		b.WriteString(styleSuccess.Render(suffixTok))
	}

	b.WriteString("\n")
	return b.String()
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

	b.WriteString(statRow("Tried", formatBig(total)) + "  " + statRow("Rate", fmt.Sprintf("%.0f/s", rate)) + "\n")
	b.WriteString(statRow("Found", fmt.Sprintf("%d/%d", found, m.cfg.Count)) + "  " + statRow("Time", fmtDuration(elapsed)) + "\n")
	b.WriteString(statRow("ETA", etaStr) + "\n\n")

	if len(m.results) > 0 {
		b.WriteString(styleSuccess.Render("Results so far:") + "\n")
		for _, r := range m.results {
			b.WriteString("  " + styleSuccess.Render("✓") + " " + styleStat.Render(truncate(r.Address, 32)) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(styleHelp.Render("ctrl+c · q  stop search"))
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

func computeETA(cfg generator.Config, found int, ratePerSec float64) time.Duration {
	if ratePerSec <= 0 {
		return 0
	}
	d := generator.HexDifficulty(cfg.Prefix, cfg.Suffix, cfg.Contains, cfg.CaseSensitive)
	if d == nil {
		return 0
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
	return styleLabel.Width(7).Render(label) + "  " + styleAccent.Render(value)
}

func patternDesc(cfg generator.Config) string {
	var parts []string
	if cfg.Prefix != "" {
		parts = append(parts, fmt.Sprintf("prefix %q", cfg.Prefix))
	}
	if cfg.Suffix != "" {
		parts = append(parts, fmt.Sprintf("suffix %q", cfg.Suffix))
	}
	if cfg.Contains != "" {
		parts = append(parts, fmt.Sprintf("contains %q", cfg.Contains))
	}
	if cfg.Regex != "" {
		parts = append(parts, fmt.Sprintf("regex %q", cfg.Regex))
	}
	return strings.Join(parts, " + ")
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	days := h / 24
	h = h % 24
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %02d:%02d:%02d", days, h, m, s)
	}
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// formatBig formats a live counter (int64) in a human-readable way.
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

// formatBigInt formats a large difficulty number (e.g. 16^8) compactly.
func formatBigInt(n *big.Int) string {
	f, _ := new(big.Float).SetInt(n).Float64()
	switch {
	case f < 1_000:
		return fmt.Sprintf("%.0f", f)
	case f < 1_000_000:
		return fmt.Sprintf("%.1fK", f/1e3)
	case f < 1_000_000_000:
		return fmt.Sprintf("%.2fM", f/1e6)
	case f < 1_000_000_000_000:
		return fmt.Sprintf("%.2fB", f/1e9)
	default:
		return fmt.Sprintf("%.2fT", f/1e12)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
