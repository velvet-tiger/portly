package render

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/velvet-tiger/portly/internal/classify"
)

// jsonListener is portly's machine-readable output shape.
//
// It is declared separately from the internal types so the wire format is an
// explicit, reviewable contract rather than an accident of internal structure.
// Renaming an internal field must not silently change portly's output.
type jsonListener struct {
	Port        uint32   `json:"port"`
	Protocol    string   `json:"protocol"`
	Addresses   []string `json:"addresses"`
	Relevance   string   `json:"relevance"`
	Reason      string   `json:"reason"`
	Runtime     string   `json:"runtime"`
	Framework   string   `json:"framework,omitempty"`
	Agent       string   `json:"agent,omitempty"`
	PID         int32    `json:"pid"`
	SiblingPIDs []int32  `json:"sibling_pids,omitempty"`
	ParentPID   int32    `json:"parent_pid"`
	Process     string   `json:"process"`
	CommandLine string   `json:"command_line,omitempty"`
	User        string   `json:"user,omitempty"`

	// WorkingDirectory is null when the OS declined to report it, which is
	// distinct from an empty string.
	WorkingDirectory *string        `json:"working_directory"`
	Project          *jsonProject   `json:"project,omitempty"`
	Container        *jsonContainer `json:"container,omitempty"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	Probe            *jsonProbe     `json:"probe,omitempty"`
}

type jsonProject struct {
	Root   string `json:"root"`
	Name   string `json:"name"`
	Marker string `json:"marker"`
}

type jsonContainer struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Image          string `json:"image"`
	Status         string `json:"status"`
	ComposeProject string `json:"compose_project,omitempty"`
}

type jsonProbe struct {
	Responded  bool   `json:"responded"`
	StatusCode int    `json:"status_code,omitempty"`
	Server     string `json:"server,omitempty"`
	PoweredBy  string `json:"powered_by,omitempty"`
	Title      string `json:"title,omitempty"`
	Failure    string `json:"failure,omitempty"`
}

type jsonDocument struct {
	ScannedAt time.Time      `json:"scanned_at"`
	Shown     int            `json:"shown"`
	Hidden    map[string]int `json:"hidden"`
	Listeners []jsonListener `json:"listeners"`
}

// JSON writes results as a single JSON document.
type JSON struct {
	now time.Time
}

// NewJSON builds a JSON renderer. now is injected so output is deterministic
// under test.
func NewJSON(now time.Time) *JSON {
	return &JSON{now: now}
}

// Write emits rows and the hidden counts as one indented JSON object.
func (j *JSON) Write(out io.Writer, rows []Row, hidden map[classify.Relevance]int) error {
	document := jsonDocument{
		ScannedAt: j.now,
		Shown:     len(rows),
		Hidden:    make(map[string]int, len(hidden)),
		Listeners: make([]jsonListener, 0, len(rows)),
	}
	for relevance, count := range hidden {
		document.Hidden[relevance.String()] = count
	}
	for _, row := range rows {
		document.Listeners = append(document.Listeners, toJSON(row))
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return fmt.Errorf("encoding the result as JSON: %w", err)
	}
	return nil
}

// toJSON converts one row to its wire shape.
func toJSON(row Row) jsonListener {
	process := row.Listener.Process

	converted := jsonListener{
		Port:        row.Listener.Port,
		Protocol:    row.Listener.Protocol,
		Addresses:   row.Listener.Addresses,
		Relevance:   row.Result.Relevance.String(),
		Reason:      row.Result.Reason,
		Runtime:     string(row.Result.Runtime),
		Framework:   row.Result.Framework,
		Agent:       row.Result.Agent,
		PID:         process.PID,
		SiblingPIDs: row.Listener.SiblingPIDs,
		ParentPID:   process.ParentPID,
		Process:     process.Name,
		CommandLine: process.CommandLine,
		User:        process.User,
	}

	if process.WorkingDir.Known {
		path := process.WorkingDir.Path
		converted.WorkingDirectory = &path
	}
	if !process.StartedAt.IsZero() {
		startedAt := process.StartedAt
		converted.StartedAt = &startedAt
	}
	if project := row.Result.Project; project != nil {
		converted.Project = &jsonProject{Root: project.Root, Name: project.Name, Marker: project.Marker}
	}
	if container := row.Listener.Container; container != nil {
		converted.Container = &jsonContainer{
			ID:             container.ID,
			Name:           container.Name,
			Image:          container.Image,
			Status:         container.Status,
			ComposeProject: container.ComposeProject,
		}
	}
	if result := row.Probe; result != nil {
		converted.Probe = &jsonProbe{
			Responded:  result.Responded,
			StatusCode: result.StatusCode,
			Server:     result.Server,
			PoweredBy:  result.PoweredBy,
			Title:      result.Title,
			Failure:    result.Failure,
		}
	}
	return converted
}
