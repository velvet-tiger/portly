package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/velvet-tiger/portly/internal/registry"
)

// PortsReport is everything `portly ports` shows.
type PortsReport struct {
	Range registry.PortRange
	// Statuses covers every port in Range, free ones included.
	Statuses []registry.PortStatus
	// StaleClaims were left out because they no longer hold. They are listed
	// so the report never drops a claim without saying so.
	StaleClaims []registry.StaleClaim
	// ClaimsOutsideRange counts live claims on ports outside Range.
	ClaimsOutsideRange int
	// FreeOnly selects free ports instead of used ones.
	FreeOnly bool
}

// selected returns the statuses the report shows.
func (r PortsReport) selected() []registry.PortStatus {
	chosen := make([]registry.PortStatus, 0, len(r.Statuses))
	for _, status := range r.Statuses {
		if status.Used() != r.FreeOnly {
			chosen = append(chosen, status)
		}
	}
	return chosen
}

// usedCount is the number of used ports in the range.
func (r PortsReport) usedCount() int {
	used := 0
	for _, status := range r.Statuses {
		if status.Used() {
			used++
		}
	}
	return used
}

// PortsTable writes a PortsReport for a terminal.
type PortsTable struct {
	palette Palette
}

// NewPortsTable builds a terminal renderer for `portly ports`.
func NewPortsTable(palette Palette) *PortsTable {
	return &PortsTable{palette: palette}
}

// Write renders the report.
//
// Free ports print one per line with nothing else on stdout, so the output
// can be piped straight into another command. Used ports print as a table
// followed by a summary of the whole range.
func (t *PortsTable) Write(out io.Writer, report PortsReport) error {
	if report.FreeOnly {
		for _, status := range report.selected() {
			if _, err := fmt.Fprintln(out, status.Port); err != nil {
				return fmt.Errorf("writing free ports: %w", err)
			}
		}
		return nil
	}

	used := report.selected()
	if len(used) == 0 {
		if _, err := fmt.Fprintln(out, t.palette.Muted.Render("Nothing in use.")); err != nil {
			return fmt.Errorf("writing the empty result: %w", err)
		}
		return t.writeSummary(out, report)
	}

	rendered := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(t.palette.Border).
		Headers("PORT", "STATE", "CLAIM", "HELD BY").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return t.palette.Header.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})

	for _, status := range used {
		rendered.Row(
			t.palette.Port.Render(fmt.Sprintf("%d", status.Port)),
			t.describeState(status.State),
			t.describeClaim(status.Claim),
			t.describeHolders(status),
		)
	}

	if _, err := fmt.Fprintln(out, rendered.Render()); err != nil {
		return fmt.Errorf("writing the table: %w", err)
	}
	return t.writeSummary(out, report)
}

// describeState renders a port state, drawing the eye to conflicts.
func (t *PortsTable) describeState(state registry.State) string {
	if state == registry.StateConflict {
		return t.palette.Port.Render(string(state))
	}
	return string(state)
}

// describeClaim names the claim on a port.
func (t *PortsTable) describeClaim(claim *registry.Claim) string {
	if claim == nil {
		return t.palette.Muted.Render("-")
	}
	return claim.Describe()
}

// describeHolders names the processes holding a port.
func (t *PortsTable) describeHolders(status registry.PortStatus) string {
	if len(status.Holders) == 0 {
		if status.State == registry.StateConflict || status.State == registry.StateUnavailable {
			return t.palette.Muted.Render("not visible to portly")
		}
		return t.palette.Muted.Render("-")
	}

	parts := make([]string, 0, len(status.Holders))
	for _, holder := range status.Holders {
		parts = append(parts, fmt.Sprintf("%s (pid %d)", holder.Process, holder.PID))
	}
	return strings.Join(parts, ", ")
}

// writeSummary reports counts for the whole range, including claims left out.
func (t *PortsTable) writeSummary(out io.Writer, report PortsReport) error {
	used := report.usedCount()
	line := fmt.Sprintf("%d used, %d free in %s.", used, len(report.Statuses)-used, report.Range)

	if count := len(report.StaleClaims); count > 0 {
		line += fmt.Sprintf(" %s ignored.", plural(count, "stale claim"))
	}
	if count := report.ClaimsOutsideRange; count > 0 {
		line += fmt.Sprintf(" %s outside this range.", plural(count, "claim"))
	}

	if _, err := fmt.Fprintln(out, t.palette.Muted.Render(line)); err != nil {
		return fmt.Errorf("writing the summary: %w", err)
	}
	return nil
}

// plural renders a count with its noun, adding an s when needed.
func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// jsonClaim is the wire shape of a claim. It matches the registry file's
// record so a claim reads the same everywhere it appears.
type jsonClaim struct {
	Port         uint32    `json:"port"`
	Project      string    `json:"project"`
	ProjectDir   string    `json:"project_dir"`
	Name         string    `json:"name"`
	RegisteredAt time.Time `json:"registered_at"`
	PID          *int32    `json:"pid"`
}

type jsonStaleClaim struct {
	jsonClaim
	Reason string `json:"reason"`
}

type jsonHolder struct {
	PID        int32  `json:"pid"`
	Process    string `json:"process"`
	ProjectDir string `json:"project_dir,omitempty"`
	Project    string `json:"project,omitempty"`
}

type jsonPortStatus struct {
	Port    uint32       `json:"port"`
	State   string       `json:"state"`
	Claim   *jsonClaim   `json:"claim,omitempty"`
	Holders []jsonHolder `json:"holders,omitempty"`
}

type jsonPortsDocument struct {
	Range              string           `json:"range"`
	Used               int              `json:"used"`
	Free               int              `json:"free"`
	ClaimsOutsideRange int              `json:"claims_outside_range"`
	StaleClaims        []jsonStaleClaim `json:"stale_claims"`
	Ports              []jsonPortStatus `json:"ports"`
}

// WritePortsJSON writes a PortsReport as one JSON document. Ports holds the
// same selection the table shows: used ports, or free ones with FreeOnly.
func WritePortsJSON(out io.Writer, report PortsReport) error {
	used := report.usedCount()
	document := jsonPortsDocument{
		Range:              report.Range.String(),
		Used:               used,
		Free:               len(report.Statuses) - used,
		ClaimsOutsideRange: report.ClaimsOutsideRange,
		StaleClaims:        make([]jsonStaleClaim, 0, len(report.StaleClaims)),
		Ports:              make([]jsonPortStatus, 0, len(report.Statuses)),
	}
	for _, stale := range report.StaleClaims {
		document.StaleClaims = append(document.StaleClaims, jsonStaleClaim{jsonClaim: claimToJSON(stale.Claim), Reason: stale.Reason})
	}
	for _, status := range report.selected() {
		document.Ports = append(document.Ports, portStatusToJSON(status))
	}
	return writeIndentedJSON(out, document)
}

// WriteClaimJSON writes one claim as a JSON object.
func WriteClaimJSON(out io.Writer, claim registry.Claim) error {
	return writeIndentedJSON(out, claimToJSON(claim))
}

func claimToJSON(claim registry.Claim) jsonClaim {
	converted := jsonClaim{
		Port:         claim.Port,
		Project:      claim.Project,
		ProjectDir:   claim.ProjectDir,
		Name:         claim.Name,
		RegisteredAt: claim.RegisteredAt,
	}
	if claim.PID != 0 {
		pid := claim.PID
		converted.PID = &pid
	}
	return converted
}

func portStatusToJSON(status registry.PortStatus) jsonPortStatus {
	converted := jsonPortStatus{Port: status.Port, State: string(status.State)}
	if status.Claim != nil {
		claim := claimToJSON(*status.Claim)
		converted.Claim = &claim
	}
	for _, holder := range status.Holders {
		converted.Holders = append(converted.Holders, jsonHolder{
			PID:        holder.PID,
			Process:    holder.Process,
			ProjectDir: holder.ProjectDir,
			Project:    holder.ProjectName,
		})
	}
	return converted
}

// writeIndentedJSON encodes value as indented JSON.
func writeIndentedJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encoding the result as JSON: %w", err)
	}
	return nil
}
