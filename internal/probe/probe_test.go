package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCandidateHosts(t *testing.T) {
	cases := []struct {
		name      string
		addresses []string
		want      []string
	}{
		{
			name:      "a server bound only to the IPv6 loopback is probed there",
			addresses: []string{"::1"},
			want:      []string{"::1"},
		},
		{
			name:      "a wildcard bind is tried on both loopback families",
			addresses: []string{"*"},
			want:      []string{"127.0.0.1", "::1"},
		},
		{
			name:      "an IPv4 loopback bind is probed on IPv4 only",
			addresses: []string{"127.0.0.1"},
			want:      []string{"127.0.0.1"},
		},
		{
			name:      "a dual bind yields both families",
			addresses: []string{"127.0.0.1", "::1"},
			want:      []string{"127.0.0.1", "::1"},
		},
		{
			name:      "a specific interface address is probed as given",
			addresses: []string{"192.168.1.20"},
			want:      []string{"192.168.1.20"},
		},
		{
			name:      "no address at all falls back to the IPv4 loopback",
			addresses: nil,
			want:      []string{"127.0.0.1"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := CandidateHosts(testCase.addresses)
			if strings.Join(got, ",") != strings.Join(testCase.want, ",") {
				t.Errorf("CandidateHosts(%v) = %v, want %v", testCase.addresses, got, testCase.want)
			}
		})
	}
}

func TestInspectReadsStatusHeadersAndTitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "test-server")
		w.Header().Set("X-Powered-By", "Express")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><head><title>  Shop\n  Admin </title></head><body>hi</body></html>"))
	}))
	defer server.Close()

	result := NewHTTPProbe(2*time.Second).Inspect(context.Background(), targetFor(t, server.URL))

	if !result.Responded {
		t.Fatalf("Responded = false, failure was %q", result.Failure)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.Server != "test-server" {
		t.Errorf("Server = %q, want test-server", result.Server)
	}
	if result.PoweredBy != "Express" {
		t.Errorf("PoweredBy = %q, want Express", result.PoweredBy)
	}
	if result.Title != "Shop Admin" {
		t.Errorf("Title = %q, want the title collapsed onto one line", result.Title)
	}
}

func TestInspectDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/elsewhere", http.StatusFound)
	}))
	defer server.Close()

	result := NewHTTPProbe(2*time.Second).Inspect(context.Background(), targetFor(t, server.URL))

	if result.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want the redirect itself rather than its target", result.StatusCode)
	}
}

func TestInspectReportsARefusedPortWithoutClaimingItResponded(t *testing.T) {
	// Port 1 on the loopback is reserved and not bound in any normal environment.
	result := NewHTTPProbe(500*time.Millisecond).Inspect(
		context.Background(),
		Target{Port: 1, Addresses: []string{"127.0.0.1"}},
	)

	if result.Responded {
		t.Error("Responded = true for a port nothing is listening on")
	}
	if result.Failure == "" {
		t.Error("a non-response must explain itself rather than render as an empty cell")
	}
}

func TestInspectAllKeysResultsByPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	target := targetFor(t, server.URL)
	results := NewHTTPProbe(2*time.Second).InspectAll(context.Background(), []Target{target})

	result, found := results[target.Port]
	if !found {
		t.Fatalf("no result for port %d", target.Port)
	}
	if result.StatusCode != http.StatusTeapot {
		t.Errorf("StatusCode = %d, want 418", result.StatusCode)
	}
}

func TestExtractTitleWithoutATitleElement(t *testing.T) {
	if got := extractTitle("<html><body>no title here</body></html>"); got != "" {
		t.Errorf("extractTitle() = %q, want empty", got)
	}
}

// targetFor turns an httptest server URL into a probe Target.
func targetFor(t *testing.T, rawURL string) Target {
	t.Helper()

	_, port, found := strings.Cut(strings.TrimPrefix(rawURL, "http://"), ":")
	if !found {
		t.Fatalf("cannot read a port from %q", rawURL)
	}
	parsed, err := strconv.ParseUint(port, 10, 32)
	if err != nil {
		t.Fatalf("cannot parse port %q: %v", port, err)
	}
	return Target{Port: uint32(parsed), Addresses: []string{"127.0.0.1"}}
}
