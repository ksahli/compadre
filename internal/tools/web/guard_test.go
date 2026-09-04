package web

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// TestPublic walks the predicate the whole fence rests on. It is written as a
// list of addresses rather than of ranges because the ranges are the
// implementation: what matters is that a name resolving to any of these is not
// somewhere this process will go.
func TestPublic(t *testing.T) {
	cases := []struct {
		address string
		want    bool
		why     string
	}{
		{"1.1.1.1", true, "a public resolver"},
		{"93.184.216.34", true, "an ordinary host"},
		{"2606:4700:4700::1111", true, "a public v6 host"},

		{"127.0.0.1", false, "loopback"},
		{"127.1.2.3", false, "the rest of loopback, which is all of 127/8"},
		{"::1", false, "loopback written as v6"},
		{"0.0.0.0", false, "unspecified"},
		{"::", false, "unspecified as v6"},
		{"10.0.0.1", false, "RFC 1918"},
		{"172.16.0.1", false, "RFC 1918"},
		{"192.168.1.1", false, "RFC 1918"},
		{"fd00::1", false, "v6 unique local"},
		{"169.254.169.254", false, "the cloud metadata service, which is the whole point"},
		{"fe80::1", false, "v6 link-local"},
		{"224.0.0.1", false, "multicast"},
		{"ff02::1", false, "v6 multicast"},
		{"100.64.0.1", false, "carrier-grade NAT, which is somebody's LAN"},
		{"192.0.0.1", false, "IETF protocol assignments"},
		{"192.0.2.1", false, "documentation"},
		{"198.18.0.1", false, "benchmarking"},
		{"198.51.100.1", false, "documentation"},
		{"203.0.113.1", false, "documentation"},
		{"240.0.0.1", false, "reserved"},
		{"255.255.255.255", false, "broadcast, which 240/4 covers"},
		{"2001:db8::1", false, "documentation"},
		{"64:ff9b::7f00:1", false, "loopback wearing a NAT64 hat"},

		// The unmap, which is the line the whole predicate turns on: a
		// check that asked the v6 spelling whether it was loopback
		// would be told no.
		{"::ffff:127.0.0.1", false, "loopback written as a mapped v4 address"},
		{"::ffff:169.254.169.254", false, "metadata written as a mapped v4 address"},
		{"::ffff:10.0.0.1", false, "RFC 1918 written as a mapped v4 address"},
		{"::ffff:1.1.1.1", true, "a public address is still public when mapped"},

		// The other v4-in-v6 spelling, which Unmap does not touch and
		// no predicate in netip has an opinion about.
		{"::127.0.0.1", false, "loopback written as a compatible v4 address"},
		{"::169.254.169.254", false, "metadata written as a compatible v4 address"},
		{"2002:7f00:1::1", false, "loopback wearing a 6to4 hat"},
		{"2002:a00:1::1", false, "RFC 1918 wearing a 6to4 hat"},
		{"0.1.2.3", false, "the rest of 'this network', of which only 0.0.0.0 is unspecified"},
	}

	for _, c := range cases {
		t.Run(c.address, func(t *testing.T) {
			addr, err := netip.ParseAddr(c.address)
			if err != nil {
				t.Fatalf("could not parse the fixture: %v", err)
			}
			if got := public(addr); got != c.want {
				t.Errorf("public(%s) = %v, want %v: %s", c.address, got, c.want, c.why)
			}
		})
	}
}

// TestPublicRejectsTheZeroAddress pins the one value that is not an address at
// all. It cannot arrive through control, which parses first, but public is a
// predicate and a predicate that says yes to nothing is a trap for the next
// caller.
func TestPublicRejectsTheZeroAddress(t *testing.T) {
	if public(netip.Addr{}) {
		t.Error("public(netip.Addr{}) = true, want false")
	}
}

// TestControl pins the guard as the dialer calls it: with a host and port,
// after resolution, about to connect.
func TestControl(t *testing.T) {
	cases := []struct {
		name    string
		address string
		message string
	}{
		{"a public address", "1.1.1.1:443", ""},
		{"a public v6 address", "[2606:4700:4700::1111]:443", ""},
		{"loopback", "127.0.0.1:443", "not a public address"},
		{"loopback as v6", "[::1]:443", "not a public address"},
		{"the metadata service", "169.254.169.254:80", "not a public address"},
		{"a private address", "10.0.0.1:8080", "not a public address"},
		{"loopback mapped into v6", "[::ffff:127.0.0.1]:443", "not a public address"},
		// A name reaching the dialer would mean resolution had not
		// happened, which is the one thing this guard is here to be
		// after. It is refused rather than resolved.
		{"a name", "example.com:443", "not an address"},
		{"no port", "1.1.1.1", "missing port"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := control("tcp", c.address, nil)
			if c.message == "" {
				if err != nil {
					t.Fatalf("control(%q) error = %v, want nil", c.address, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("control(%q) = nil, want an error", c.address)
			}
			if !strings.Contains(err.Error(), c.message) {
				t.Errorf("control(%q) error = %v, want it to mention %q", c.address, err, c.message)
			}
		})
	}
}

// TestReachable pins the cheap half of the fence: what can be judged from the
// spelling alone, before anything is dialled.
func TestReachable(t *testing.T) {
	cases := []struct {
		name    string
		address string
		message string
	}{
		{"https", "https://example.com/doc", ""},
		{"https with a port", "https://example.com:8443/doc", ""},
		{"plain http", "http://example.com", "only https can be fetched, got 'http'"},
		{"a file url", "file:///etc/passwd", "only https can be fetched, got 'file'"},
		{"gopher", "gopher://example.com", "only https can be fetched, got 'gopher'"},
		{"no scheme", "example.com/doc", "the url has no scheme"},
		{"no host", "https:///doc", "the url has no host"},
		{"credentials", "https://user:secret@example.com", "a url carrying credentials is refused"},
		{"a bare username", "https://user@example.com", "a url carrying credentials is refused"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			address, err := url.Parse(c.address)
			if err != nil {
				t.Fatalf("could not parse the fixture: %v", err)
			}

			err = reachable(address)
			if c.message == "" {
				if err != nil {
					t.Fatalf("reachable(%q) error = %v, want nil", c.address, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("reachable(%q) = nil, want an error", c.address)
			}
			if got := err.Error(); !strings.Contains(got, c.message) {
				t.Errorf("reachable(%q) error = %q, want it to contain %q", c.address, got, c.message)
			}
		})
	}
}

// TestReachableRedactsTheSecret pins that refusing a url carrying credentials
// does not then repeat them. The error goes to the model and from there into a
// transcript, which is no place for a password.
func TestReachableRedactsTheSecret(t *testing.T) {
	address, err := url.Parse("https://user:hunter2@example.com/doc")
	if err != nil {
		t.Fatalf("could not parse the fixture: %v", err)
	}

	err = reachable(address)
	if err == nil {
		t.Fatal("reachable() = nil, want an error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("reachable() error = %q, want the secret redacted", err)
	}
}

// TestRedirectsGiveUp pins the ceiling on a chain, and pins it where the
// error says it is. The via slice is what the client hands the policy: one
// entry per request already made, the first of which was nobody's redirect.
func TestRedirectsGiveUp(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatalf("could not build the fixture: %v", err)
	}

	// The original request plus the five hops it was sent on: the sixth is
	// the one refused.
	via := make([]*http.Request, maxHops+1)
	err = redirects(request, via)
	if err == nil {
		t.Fatal("redirects() = nil, want an error")
	}
	if got, want := err.Error(), "gave up after 5 redirects"; got != want {
		t.Errorf("redirects() error = %q, want %q", got, want)
	}

	// The fifth hop is the last one allowed, and it is allowed.
	if err := redirects(request, via[:maxHops]); err != nil {
		t.Errorf("redirects() error = %v, want the fifth hop allowed", err)
	}
}

// TestRedirectsJudgeEachHop pins that a hop is put through the same spelling
// check the first url was, so a public https url cannot bounce the fetch
// somewhere plain or nameless.
func TestRedirectsJudgeEachHop(t *testing.T) {
	cases := []struct {
		name    string
		to      string
		message string
	}{
		{"to https", "https://elsewhere.example/doc", ""},
		{"to plain http", "http://elsewhere.example/doc", "only https can be fetched"},
		{"to a credentialed url", "https://user:pw@elsewhere.example/", "a url carrying credentials is refused"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, c.to, nil)
			if err != nil {
				t.Fatalf("could not build the fixture: %v", err)
			}

			err = redirects(request, nil)
			if c.message == "" {
				if err != nil {
					t.Fatalf("redirects() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("redirects() = nil, want an error")
			}
			if got := err.Error(); !strings.Contains(got, c.message) {
				t.Errorf("redirects() error = %q, want it to contain %q", got, c.message)
			}
		})
	}
}

// TestTheGuardIsWiredIntoTheDefaultClient is the test the rest of this file
// cannot stand in for. Everything above judges the guard on its own terms,
// which proves the predicate and proves nothing about whether the client
// actually consults it — and WithHTTPClient exists precisely to hand the tool
// a client that does not.
//
// So this one drives the default client, the one New builds, at a real server
// on loopback. The server answers happily; the point is that nothing ever
// reaches it, because the dialer refused the address after resolving it.
func TestTheGuardIsWiredIntoTheDefaultClient(t *testing.T) {
	reached := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("the model should never read this"))
	}))
	defer server.Close()

	// https and a host that is there, so the cheap half of the fence has
	// nothing to say about it. Only the dialer can refuse this url.
	out, err := New().Execute(t.Context(), []byte(`{"url":"`+server.URL+`"}`))
	if err == nil {
		t.Fatalf("Execute() = %q, want an error", out)
	}
	if got := err.Error(); !strings.Contains(got, "not a public address") {
		t.Errorf("Execute() error = %q, want the address guard to have refused it", got)
	}
	if reached {
		t.Error("the server was reached, want the dial refused before it")
	}
}
