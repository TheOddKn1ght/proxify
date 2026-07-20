package router

import (
	"context"
	"net"
	"testing"
)

type fakeDialer string

func (fakeDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	m, err := NewDomainSuffix([]string{"ru", ".su", "рф"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(
		[]Rule{{Matcher: m, Route: "direct"}},
		map[string]Dialer{"direct": fakeDialer("direct"), "socks": fakeDialer("socks")},
		"socks",
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRouteFor(t *testing.T) {
	r := newTestRouter(t)
	tests := []struct {
		host string
		want string
	}{
		{"mail.ru", "direct"},
		{"mail.ru:443", "direct"},
		{"ru", "direct"},
		{"MAIL.RU:443", "direct"},
		{"kernel.su", "direct"},
		{"кто.рф", "direct"},
		{"xn--80asehdb.xn--p1ai", "direct"},
		{"xn--80asehdb.xn--p1ai:443", "direct"},
		{"guru.com", "socks"},
		{"peru", "socks"},
		{"google.com", "socks"},
		{"google.com:443", "socks"},
		{"192.168.1.1:80", "socks"},
		{"[2001:db8::1]:443", "socks"},
	}
	for _, tt := range tests {
		route, dialer := r.RouteFor(tt.host)
		if route != tt.want {
			t.Errorf("RouteFor(%q) = %q, want %q", tt.host, route, tt.want)
		}
		if dialer == nil {
			t.Errorf("RouteFor(%q): nil dialer", tt.host)
		}
	}
}

func TestNewValidatesRoutes(t *testing.T) {
	m, _ := NewDomainSuffix([]string{"ru"})
	ups := map[string]Dialer{"direct": fakeDialer("direct")}
	if _, err := New(nil, ups, "missing"); err == nil {
		t.Error("expected error for unknown default route")
	}
	if _, err := New([]Rule{{Matcher: m, Route: "missing"}}, ups, "direct"); err == nil {
		t.Error("expected error for rule with unknown route")
	}
}
