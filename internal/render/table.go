// Package render turns scan results into output for a terminal or for another
// program.
package render

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/velvet-tiger/portly/internal/classify"
	"github.com/velvet-tiger/portly/internal/probe"
	"github.com/velvet-tiger/portly/internal/scan"
)

// Row pairs a listener with everything concluded about it.
type Row struct {
	Listener scan.Listener
	Result   classify.Result
	// Probe is set only when probing was requested.
	Probe *probe.Result
}

// Palette holds the colours the table uses. Colours are grouped here so the
// no-colour path is a single substitution rather than a branch at every use.
type Palette struct {
	Header    lipgloss.Style
	Port      lipgloss.Style
	Muted     lipgloss.Style
	Agent     lipgloss.Style
	Container lipgloss.Style
	Border    lipgloss.Style
}

// ColourPalette returns the styled palette.
func ColourPalette() Palette {
	return Palette{
		Header:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		Port:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213")),
		Muted:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		Agent:     lipgloss.NewStyle().Foreground(lipgloss.Color("120")),
		Container: lipgloss.NewStyle().Foreground(lipgloss.Color("117")),
		Border:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
}

// PlainPalette returns an unstyled palette for pipes, dumb terminals and
// NO_COLOR.
func PlainPalette() Palette {
	plain := lipgloss.NewStyle()
	return Palette{
		Header:    plain,
		Port:      plain,
		Muted:     plain,
		Agent:     plain,
		Container: plain,
		Border:    plain,
	}
}

// Table writes rows as a bordered table.
type Table struct {
	palette Palette
	now     time.Time
	probed  bool
}

// NewTable builds a table renderer. now is injected so uptime is deterministic
// under test.
func NewTable(palette Palette, now time.Time, probed bool) *Table {
	return &Table{palette: palette, now: now, probed: probed}
}

// Write renders rows, then a summary of what was hidden.
//
// hidden counts listeners excluded by the default filter. It is always printed,
// including when it is zero, so the filter never removes rows silently.
func (t *Table) Write(out io.Writer, rows []Row, hidden map[classify.Relevance]int) error {
	if len(rows) == 0 {
		if _, err := fmt.Fprintln(out, t.palette.Muted.Render("No listening ports matched.")); err != nil {
			return fmt.Errorf("writing the empty result: %w", err)
		}
		return t.writeSummary(out, 0, hidden)
	}

	headers := []string{"PORT", "WHAT", "PROJECT", "PID", "UP", "WHY"}
	if t.probed {
		headers = append(headers, "SERVING")
	}

	rendered := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(t.palette.Border).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return t.palette.Header.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})

	for _, row := range rows {
		rendered.Row(t.cells(row)...)
	}

	if _, err := fmt.Fprintln(out, rendered.Render()); err != nil {
		return fmt.Errorf("writing the table: %w", err)
	}
	return t.writeSummary(out, len(rows), hidden)
}

// cells builds one table row.
func (t *Table) cells(row Row) []string {
	cells := []string{
		t.palette.Port.Render(fmt.Sprintf("%d", row.Listener.Port)),
		t.describeWhat(row),
		t.describeProject(row),
		t.describePID(row.Listener),
		t.describeUptime(row.Listener.Process.StartedAt),
		t.describeWhy(row),
	}
	if t.probed {
		cells = append(cells, t.describeServing(row.Probe))
	}
	return cells
}

// describeWhat names the runtime and framework behind a port.
func (t *Table) describeWhat(row Row) string {
	if container := row.Listener.Container; container != nil {
		return t.palette.Container.Render(container.Name)
	}

	parts := make([]string, 0, 2)
	if row.Result.Runtime != classify.RuntimeUnknown {
		parts = append(parts, string(row.Result.Runtime))
	}
	if row.Result.Framework != "" {
		parts = append(parts, row.Result.Framework)
	}
	if len(parts) == 0 {
		return row.Listener.Process.Name
	}
	return strings.Join(parts, " ")
}

// describeProject names the codebase a process is running in.
func (t *Table) describeProject(row Row) string {
	if row.Result.Project != nil {
		return row.Result.Project.Name
	}
	if container := row.Listener.Container; container != nil && container.ComposeProject != "" {
		return container.ComposeProject
	}
	if !row.Listener.Process.WorkingDir.Known {
		return t.palette.Muted.Render("unknown")
	}
	return t.palette.Muted.Render("-")
}

// describePID renders the PID, noting a worker pool sharing the port.
func (t *Table) describePID(listener scan.Listener) string {
	if count := len(listener.SiblingPIDs); count > 0 {
		return fmt.Sprintf("%d %s", listener.Process.PID,
			t.palette.Muted.Render(fmt.Sprintf("+%d", count)))
	}
	return fmt.Sprintf("%d", listener.Process.PID)
}

// describeUptime renders how long a process has been running.
func (t *Table) describeUptime(startedAt time.Time) string {
	if startedAt.IsZero() {
		return t.palette.Muted.Render("?")
	}

	elapsed := t.now.Sub(startedAt)
	if elapsed < 0 {
		return t.palette.Muted.Render("?")
	}

	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	}
}

// describeWhy renders the rule that decided a row's classification.
func (t *Table) describeWhy(row Row) string {
	if row.Result.Agent != "" {
		return t.palette.Agent.Render(row.Result.Reason)
	}
	return t.palette.Muted.Render(row.Result.Reason)
}

// describeServing renders a probe result.
func (t *Table) describeServing(result *probe.Result) string {
	if result == nil {
		return t.palette.Muted.Render("-")
	}
	if !result.Responded {
		return t.palette.Muted.Render(result.Failure)
	}

	parts := []string{fmt.Sprintf("%d", result.StatusCode)}
	if result.Title != "" {
		parts = append(parts, truncate(result.Title, 40))
	} else if result.Server != "" {
		parts = append(parts, result.Server)
	} else if result.PoweredBy != "" {
		parts = append(parts, result.PoweredBy)
	}
	return strings.Join(parts, " ")
}

// writeSummary reports the counts, including what the default filter removed.
func (t *Table) writeSummary(out io.Writer, shown int, hidden map[classify.Relevance]int) error {
	total := 0
	for _, count := range hidden {
		total += count
	}
	if total == 0 {
		return nil
	}

	// Largest group first, so the dominant reason for a short table is the first
	// thing read. Equal counts fall back to name for a stable order.
	kinds := make([]classify.Relevance, 0, len(hidden))
	for relevance := range hidden {
		kinds = append(kinds, relevance)
	}
	sort.Slice(kinds, func(a, b int) bool {
		if hidden[kinds[a]] != hidden[kinds[b]] {
			return hidden[kinds[a]] > hidden[kinds[b]]
		}
		return kinds[a].String() < kinds[b].String()
	})

	parts := make([]string, 0, len(kinds))
	for _, relevance := range kinds {
		parts = append(parts, fmt.Sprintf("%d %s", hidden[relevance], relevance))
	}

	line := fmt.Sprintf("%d shown, %d hidden (%s). Use --all to see them.",
		shown, total, strings.Join(parts, ", "))

	if _, err := fmt.Fprintln(out, t.palette.Muted.Render(line)); err != nil {
		return fmt.Errorf("writing the summary: %w", err)
	}
	return nil
}

// truncate shortens text to at most limit runes, marking the cut.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
