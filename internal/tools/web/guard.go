package web

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

const (
	// maxHops is the ceiling on a redirect chain. Five is enough for the
	// ways a site legitimately moves a reader — a scheme fix, a canonical
	// host, a trailing slash, a locale — and short of a chain built to
	// waste the time of whoever follows it.
	maxHops = 5

	// The clocks. timeout bounds the whole fetch, redirects included; the
	// others bound the stages a server can hang in without ever tripping a
	// whole-request budget on its own.
	timeout   = 15 * time.Second
	dialing   = 5 * time.Second
	handshake = 5 * time.Second
	headers   = 10 * time.Second
)

// reserved are the ranges [net/netip.Addr] has no predicate for. IsPrivate
// covers RFC 1918 and IPv6 ULA, and the rest of the standard predicates cover
// loopback, link-local and multicast; these are the leftovers, and the first
// of them is the one that matters — a cloud instance's metadata service is
// reached over link-local, and carrier-grade NAT space is somebody's LAN.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network", of which IsUnspecified catches only 0.0.0.0
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and 255.255.255.255 with it
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64, which is a v4 address wearing a hat
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4, which carries a v4 address anyone may pick
	netip.MustParsePrefix("::/96"),           // deprecated v4-compatible, '::127.0.0.1' among it
}

// public says whether an address is somewhere on the open internet, which is
// the only place this tool will go. It is written as a refusal list rather
// than an allow list because the open internet is not enumerable and the
// things worth keeping a model away from are.
//
// The unmap is the first line and not an afterthought: '::ffff:127.0.0.1' is
// loopback written as a v6 address, and a check that asked the v6 form whether
// it was loopback would be told no. That is the mapped spelling and it is the
// only one Unmap knows; the compatible spelling '::127.0.0.1' is a different
// range that nothing in netip has a predicate for, which is what the '::/96'
// below is there for.
func public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return false
	}

	switch {
	case addr.IsLoopback(),
		addr.IsPrivate(),
		addr.IsUnspecified(),
		addr.IsMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast():
		return false
	}

	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return false
		}
	}

	return true
}

// control is the guard, and it sits at the socket rather than at the string
// for a reason worth stating. A hostname is not an address: it is a question
// asked of a resolver, and the answer can differ between the moment a name is
// checked and the moment it is dialled, or between the first hop of a redirect
// and the second. [net.Dialer.Control] runs after resolution with the concrete
// address about to be connected to, on every attempt — so what is judged here
// is the thing actually reached, the same insistence as the file tools judging
// a path only after it has been resolved through its symlinks.
func control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("refusing to reach '%s': %w", address, err)
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("refusing to reach '%s': not an address", host)
	}
	if !public(addr) {
		return fmt.Errorf("refusing to reach %s: not a public address", host)
	}

	return nil
}

// reachable judges a URL as written. It is the cheap half of the fence — the
// half that can answer before anything is dialled — and it is deliberately
// about the spelling only. Where the name leads is control's business.
func reachable(address *url.URL) error {
	switch address.Scheme {
	case "https":
	case "":
		return fmt.Errorf("the url has no scheme, and only https can be fetched: '%s'", address)
	default:
		return fmt.Errorf("only https can be fetched, got '%s'", address.Scheme)
	}
	if address.Host == "" {
		return fmt.Errorf("the url has no host: '%s'", address)
	}
	// Credentials in a URL are a way to hand a secret to whatever the name
	// happens to resolve to, and a model that has one has been given it by
	// something it read.
	if address.User != nil {
		return fmt.Errorf("a url carrying credentials is refused: '%s'", address.Redacted())
	}

	return nil
}

// redirects is the policy on being sent elsewhere. Each hop is put through
// reachable again, so a public https URL cannot bounce the fetch into
// somewhere plain or nameless; the address guard needs no repeating here,
// since the dialer applies to every connection the chain opens.
// via holds the requests already made, the first of which is the one nobody
// redirected to — so the number of hops actually followed is one less than its
// length, and the ceiling is compared against that rather than against the
// count of requests. The alternative is an error that says five and means
// four, and a ceiling that lies about itself is worse than a lower one.
func redirects(request *http.Request, via []*http.Request) error {
	if followed := len(via) - 1; followed >= maxHops {
		return fmt.Errorf("gave up after %d redirects", maxHops)
	}
	return reachable(request.URL)
}

// guarded builds the client this tool fetches with: the address guard in its
// dialer, the redirect policy above it, and a clock on every stage a server
// could otherwise hold open.
func guarded() *http.Client {
	dialer := &net.Dialer{Timeout: dialing, Control: control}

	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: redirects,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   handshake,
			ResponseHeaderTimeout: headers,
			ForceAttemptHTTP2:     true,
		},
	}
}
