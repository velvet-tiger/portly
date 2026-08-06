// Package probe optionally asks a listening port what it is.
//
// Probing is the only part of portly with an outward effect: it opens a
// connection and sends a request to a local service. That can wake an idle dev
// server and will appear in its access log, so it is off unless asked for.
package probe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// bodyReadLimit caps how much of a response is read. A title lives in the first
// few hundred bytes, and an unbounded read on an endpoint streaming a log file
// would never return.
const bodyReadLimit = 64 << 10

// maxConcurrentProbes bounds parallelism so a machine with many open ports does
// not open all its connections at once.
const maxConcurrentProbes = 12

// titlePattern extracts the contents of the first HTML title element.
var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// Result is what a port said when asked.
type Result struct {
	// Responded is false when the port refused, timed out, or spoke something
	// other than HTTP. The port is still open; it simply is not a web server.
	Responded  bool
	StatusCode int
	Server     string
	PoweredBy  string
	Title      string
	// Failure explains a non-response, so a blank row is never mistaken for a
	// probe that was not attempted.
	Failure string
}

// HTTPProbe sends one GET request to a local port.
type HTTPProbe struct {
	client *http.Client
}

// NewHTTPProbe builds a probe whose requests give up after timeout.
//
// Redirects are not followed: a redirect target is another port's business, and
// following one would send traffic somewhere the caller did not ask about.
func NewHTTPProbe(timeout time.Duration) *HTTPProbe {
	return &HTTPProbe{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				DisableKeepAlives: true,
				DialContext:       (&net.Dialer{Timeout: timeout}).DialContext,
			},
		},
	}
}

// Target is one port to probe, together with the addresses it is bound to.
type Target struct {
	Port uint32
	// Addresses are the listener's bind addresses as the socket table reports
	// them, such as "127.0.0.1", "::1" or "*".
	Addresses []string
}

// Inspect asks one port for its root document.
//
// The host is derived from the listener's bind addresses. Probing a fixed
// 127.0.0.1 reports "connection refused" for any server bound only to ::1,
// which Vite does by default, and that failure looks identical to a dead port.
func (p *HTTPProbe) Inspect(ctx context.Context, target Target) Result {
	hosts := CandidateHosts(target.Addresses)

	var last Result
	for _, host := range hosts {
		last = p.request(ctx, host, target.Port)
		if last.Responded {
			return last
		}
	}
	return last
}

// request performs one GET against a single host and port.
func (p *HTTPProbe) request(ctx context.Context, host string, port uint32) Result {
	address := fmt.Sprintf("http://%s/", net.JoinHostPort(host, strconv.FormatUint(uint64(port), 10)))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return Result{Failure: fmt.Sprintf("building the request: %v", err)}
	}
	request.Header.Set("User-Agent", "portly")

	response, err := p.client.Do(request)
	if err != nil {
		return Result{Failure: summariseFailure(err)}
	}
	defer response.Body.Close()

	result := Result{
		Responded:  true,
		StatusCode: response.StatusCode,
		Server:     response.Header.Get("Server"),
		PoweredBy:  response.Header.Get("X-Powered-By"),
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, bodyReadLimit))
	if err == nil {
		result.Title = extractTitle(string(body))
	}
	return result
}

// InspectAll probes many targets concurrently and returns results keyed by port.
func (p *HTTPProbe) InspectAll(ctx context.Context, targets []Target) map[uint32]Result {
	results := make(map[uint32]Result, len(targets))
	var guard sync.Mutex
	var group sync.WaitGroup

	slots := make(chan struct{}, maxConcurrentProbes)

	for _, target := range targets {
		group.Add(1)
		go func(target Target) {
			defer group.Done()

			slots <- struct{}{}
			defer func() { <-slots }()

			result := p.Inspect(ctx, target)

			guard.Lock()
			results[target.Port] = result
			guard.Unlock()
		}(target)
	}

	group.Wait()
	return results
}

// CandidateHosts turns bind addresses into the hosts worth probing, in order.
//
// A wildcard bind yields both loopback families because the socket table does
// not record whether a wildcard socket accepts IPv4, and on macOS an IPv6
// wildcard usually does. A concrete address is probed as given.
func CandidateHosts(addresses []string) []string {
	var wildcard, ipv4, ipv6, specific bool
	var specificHosts []string

	for _, address := range addresses {
		switch address {
		case "", "*":
			wildcard = true
		case "0.0.0.0", "127.0.0.1":
			ipv4 = true
		case "::", "::1":
			ipv6 = true
		default:
			specific = true
			specificHosts = append(specificHosts, address)
		}
	}

	var hosts []string
	if ipv4 || wildcard {
		hosts = append(hosts, "127.0.0.1")
	}
	if ipv6 || wildcard {
		hosts = append(hosts, "::1")
	}
	if specific {
		hosts = append(hosts, specificHosts...)
	}
	if len(hosts) == 0 {
		return []string{"127.0.0.1"}
	}
	return hosts
}

// extractTitle pulls a page title out of an HTML document and collapses its
// whitespace onto one line.
func extractTitle(body string) string {
	match := titlePattern.FindStringSubmatch(body)
	if match == nil {
		return ""
	}
	return strings.Join(strings.Fields(match[1]), " ")
}

// summariseFailure turns a transport error into a short readable cause.
func summariseFailure(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "connection refused"):
		return "connection refused"
	case strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded"):
		return "timed out"
	case strings.Contains(message, "malformed HTTP response"):
		return "not http"
	case strings.Contains(message, "EOF"):
		return "closed without replying"
	default:
		return message
	}
}
