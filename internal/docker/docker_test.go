package docker

import "testing"

// dockerPSLine builds one line in the format ByHostPort asks the docker CLI for.
func dockerPSLine(id, name, image, ports, status, project string) string {
	fields := []string{id, name, image, ports, status, project}
	line := ""
	for i, field := range fields {
		if i > 0 {
			line += fieldSeparator
		}
		line += field
	}
	return line + "\n"
}

func TestParsePortMappings(t *testing.T) {
	output := dockerPSLine(
		"e1ae9290128ddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"kelpie-crm-db-1", "postgres:18-alpine",
		"0.0.0.0:52349->5432/tcp, [::]:52349->5432/tcp",
		"Up 4 minutes (healthy)", "kelpie-crm",
	) + dockerPSLine(
		"dce547913252", "consultmed-web-1", "node:lts-alpine",
		"0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp",
		"Up 8 minutes", "consultmed",
	)

	byPort := parsePortMappings(output)

	if len(byPort) != 2 {
		t.Fatalf("got %d mapped ports, want 2; the IPv4 and IPv6 entries for one port are one mapping", len(byPort))
	}

	postgres, found := byPort[52349]
	if !found {
		t.Fatal("host port 52349 was not mapped")
	}
	if postgres.Name != "kelpie-crm-db-1" {
		t.Errorf("Name = %q, want kelpie-crm-db-1", postgres.Name)
	}
	if postgres.ComposeProject != "kelpie-crm" {
		t.Errorf("ComposeProject = %q, want kelpie-crm", postgres.ComposeProject)
	}
	if len(postgres.ID) != 12 {
		t.Errorf("ID = %q, want it trimmed to 12 characters", postgres.ID)
	}
}

func TestParsePortMappingsIgnoresContainersPublishingNothing(t *testing.T) {
	output := dockerPSLine("abc123", "worker-1", "worker:latest", "", "Up 2 minutes", "shop")

	if byPort := parsePortMappings(output); len(byPort) != 0 {
		t.Errorf("got %d mapped ports, want none for a container with no published ports", len(byPort))
	}
}

func TestParsePortMappingsSkipsMalformedLines(t *testing.T) {
	output := "not a real line\n" +
		dockerPSLine("abc123", "api-1", "api:latest", "0.0.0.0:8000->8000/tcp", "Up", "shop")

	byPort := parsePortMappings(output)

	if len(byPort) != 1 {
		t.Fatalf("got %d mapped ports, want the one well-formed line", len(byPort))
	}
	if byPort[8000].Name != "api-1" {
		t.Errorf("Name = %q, want api-1", byPort[8000].Name)
	}
}

func TestParsePortMappingsHandlesAContainerPortWithNoHostPort(t *testing.T) {
	// An unpublished port appears without a host mapping and must not be read as
	// a host port, or portly would attribute an unrelated local port to it.
	output := dockerPSLine("abc123", "db-1", "postgres:16", "5432/tcp", "Up", "shop")

	if byPort := parsePortMappings(output); len(byPort) != 0 {
		t.Errorf("got %v, want no host ports for an unpublished container port", byPort)
	}
}

func TestParsePortMappingsReadsAnIPv6OnlyPublication(t *testing.T) {
	output := dockerPSLine("abc123", "api-1", "api:latest", "[::]:9100->9100/tcp", "Up", "shop")

	byPort := parsePortMappings(output)
	if byPort[9100].Name != "api-1" {
		t.Errorf("got %v, want host port 9100 mapped to api-1", byPort)
	}
}
