package registry

import (
	"fmt"
	"strconv"
	"strings"
)

// PortRange is an inclusive span of ports.
type PortRange struct {
	First uint32
	Last  uint32
}

// DefaultRange is where ports are allocated when no range is given. It avoids
// 3000, 5173, 8000 and 8080, which frameworks pick by default.
var DefaultRange = PortRange{First: 4000, Last: 4999}

// ParsePortRange reads a range written as "first-last", such as "4000-4999".
func ParsePortRange(text string) (PortRange, error) {
	firstText, lastText, found := strings.Cut(text, "-")
	if !found {
		return PortRange{}, fmt.Errorf("%q is not a port range: write it as first-last, such as 4000-4999", text)
	}

	first, err := ParsePort(firstText)
	if err != nil {
		return PortRange{}, fmt.Errorf("%q is not a port range: %w", text, err)
	}
	last, err := ParsePort(lastText)
	if err != nil {
		return PortRange{}, fmt.Errorf("%q is not a port range: %w", text, err)
	}
	if first > last {
		return PortRange{}, fmt.Errorf("%q is not a port range: %d is above %d", text, first, last)
	}
	return PortRange{First: first, Last: last}, nil
}

// ParsePort reads one TCP port, rejecting 0 and anything above 65535.
func ParsePort(text string) (uint32, error) {
	port, err := strconv.ParseUint(text, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid port: %w", text, err)
	}
	if port == 0 {
		return 0, fmt.Errorf("%q is not a valid port: ports run from 1 to 65535", text)
	}
	return uint32(port), nil
}

// Contains reports whether port lies inside the range.
func (r PortRange) Contains(port uint32) bool {
	return port >= r.First && port <= r.Last
}

// Size is the number of ports in the range.
func (r PortRange) Size() int {
	return int(r.Last-r.First) + 1
}

// String renders the range in the form ParsePortRange reads.
func (r PortRange) String() string {
	return fmt.Sprintf("%d-%d", r.First, r.Last)
}
