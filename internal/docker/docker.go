// Package docker attributes published host ports to the containers behind them.
//
// Docker's published ports appear in the host socket table as the Docker
// backend process. That process carries no container name, image or project, so
// container rows are unreadable without this lookup.
package docker

import (
	"bufio"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/velvet-tiger/portly/internal/scan"
)

// fieldSeparator is an unlikely sequence in container names, images or status
// strings, which keeps the format parseable without quoting rules.
const fieldSeparator = "\x1f"

// lookupTimeout bounds the CLI call. A hung Docker daemon must not hang portly.
const lookupTimeout = 3 * time.Second

// hostPortPattern matches the host port in one entry of docker's port mapping
// column, such as "0.0.0.0:8000->8000/tcp" or "[::]:52349->5432/tcp".
var hostPortPattern = regexp.MustCompile(`(?:^|\s)(?:[0-9.]+|\[[0-9a-fA-F:]*\]):(\d+)->`)

// CommandIndex reads container state by running the docker CLI.
//
// The CLI is used rather than the Engine API socket because it already resolves
// the active context, which may be Docker Desktop, Colima, or a remote host.
type CommandIndex struct {
	executable string
}

// NewIndex returns a ContainerIndex backed by the docker CLI.
func NewIndex() *CommandIndex {
	return &CommandIndex{executable: "docker"}
}

// ByHostPort returns running containers keyed by each host port they publish.
//
// A missing docker binary or a stopped daemon returns an empty map and no
// error. Neither is a failure of portly, and neither should stop a scan that is
// otherwise complete.
func (i *CommandIndex) ByHostPort(ctx context.Context) (map[uint32]scan.Container, error) {
	binary, err := exec.LookPath(i.executable)
	if err != nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	format := strings.Join([]string{
		"{{.ID}}", "{{.Names}}", "{{.Image}}", "{{.Ports}}", "{{.Status}}", "{{.Label \"com.docker.compose.project\"}}",
	}, fieldSeparator)

	output, err := exec.CommandContext(ctx, binary, "ps", "--no-trunc", "--format", format).Output()
	if err != nil {
		// A daemon that is not running exits non-zero. That is an ordinary state
		// on a machine where the user has not started Docker today.
		return nil, nil
	}

	return parsePortMappings(string(output)), nil
}

// parsePortMappings converts docker ps output into a host-port lookup.
//
// It is separated from the CLI call so the parsing rules can be tested against
// recorded output without a running daemon.
func parsePortMappings(output string) map[uint32]scan.Container {
	byPort := make(map[uint32]scan.Container)

	lines := bufio.NewScanner(strings.NewReader(output))
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" {
			continue
		}

		fields := strings.Split(line, fieldSeparator)
		if len(fields) < 5 {
			continue
		}

		container := scan.Container{
			ID:     shortID(fields[0]),
			Name:   fields[1],
			Image:  fields[2],
			Status: fields[4],
		}
		if len(fields) >= 6 {
			container.ComposeProject = fields[5]
		}

		for _, port := range hostPorts(fields[3]) {
			byPort[port] = container
		}
	}
	return byPort
}

// hostPorts extracts every published host port from docker's port column.
//
// A container published on both IPv4 and IPv6 lists the same host port twice,
// which collapses naturally because the result keys a map.
func hostPorts(column string) []uint32 {
	matches := hostPortPattern.FindAllStringSubmatch(column, -1)
	ports := make([]uint32, 0, len(matches))

	for _, match := range matches {
		value, err := strconv.ParseUint(match[1], 10, 32)
		if err != nil {
			continue
		}
		ports = append(ports, uint32(value))
	}
	return ports
}

// shortID trims a full container ID to the 12 characters docker displays.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
