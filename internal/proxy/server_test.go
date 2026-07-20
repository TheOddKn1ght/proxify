package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"proxify/internal/router"
	"proxify/internal/socks5"
)

type rewriteDialer struct {
	backend string
	mu      sync.Mutex
	addrs   []string
}

func (d *rewriteDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.addrs = append(d.addrs, addr)
	d.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, network, d.backend)
}

func (d *rewriteDialer) requested() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.addrs...)
}

func startFakeSocks(t *testing.T, backend string) (addr string, targets *sync.Map) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	targets = &sync.Map{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				head := make([]byte, 2)
				if _, err := io.ReadFull(conn, head); err != nil {
					return
				}
				methods := make([]byte, head[1])
				io.ReadFull(conn, methods)
				conn.Write([]byte{0x05, 0x00})

				req := make([]byte, 4)
				if _, err := io.ReadFull(conn, req); err != nil {
					return
				}
				if req[3] != 0x03 {
					return
				}
				var l [1]byte
				io.ReadFull(conn, l[:])
				name := make([]byte, l[0])
				io.ReadFull(conn, name)
				var port [2]byte
				io.ReadFull(conn, port[:])
				targets.Store(string(name), true)

				conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})
				out, err := net.Dial("tcp", backend)
				if err != nil {
					return
				}
				defer out.Close()
				go io.Copy(out, conn)
				io.Copy(conn, out)
			}()
		}
	}()
	return ln.Addr().String(), targets
}

func startProxy(t *testing.T, backend string) (proxyURL string, direct *rewriteDialer, socksTargets *sync.Map) {
	t.Helper()
	socksAddr, socksTargets := startFakeSocks(t, backend)
	direct = &rewriteDialer{backend: backend}

	m, err := router.NewDomainSuffix([]string{"ru"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.New(
		[]router.Rule{{Matcher: m, Route: "direct"}},
		map[string]router.Dialer{
			"direct": direct,
			"socks":  &socks5.Client{Addr: socksAddr, Timeout: 5 * time.Second},
		},
		"socks",
	)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(r, slog.New(slog.DiscardHandler), 5*time.Second))
	t.Cleanup(srv.Close)
	return srv.URL, direct, socksTargets
}

func proxyClient(t *testing.T, proxyURL string) *http.Client {
	t.Helper()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 10 * time.Second}
}

func TestPlainHTTPRouting(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s", r.Host)
	}))
	defer target.Close()
	targetAddr := target.Listener.Addr().String()

	proxyURL, direct, socksTargets := startProxy(t, targetAddr)
	client := proxyClient(t, proxyURL)

	resp, err := client.Get("http://mail.ru/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hello from mail.ru" {
		t.Errorf("body = %q", body)
	}
	if got := direct.requested(); len(got) != 1 || got[0] != "mail.ru:80" {
		t.Errorf("direct dialer saw %v, want [mail.ru:80]", got)
	}
	if _, ok := socksTargets.Load("mail.ru"); ok {
		t.Error("mail.ru leaked to the SOCKS server")
	}

	resp, err = client.Get("http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if _, ok := socksTargets.Load("example.com"); !ok {
		t.Error("example.com did not go through the SOCKS server")
	}
	if got := direct.requested(); len(got) != 1 {
		t.Errorf("direct dialer saw %v, want only mail.ru", got)
	}
}

func TestConnectTunnel(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()

	proxyURL, direct, socksTargets := startProxy(t, echo.Addr().String())
	u, _ := url.Parse(proxyURL)

	connectAndEcho := func(target string) {
		t.Helper()
		conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("CONNECT %s: status %d", target, resp.StatusCode)
		}
		conn.Write([]byte("ping"))
		buf := make([]byte, 4)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.ReadFull(br, buf); err != nil {
			t.Fatal(err)
		}
		if string(buf) != "ping" {
			t.Fatalf("echo through tunnel = %q", buf)
		}
	}

	connectAndEcho("mail.ru:443")
	if got := direct.requested(); len(got) != 1 || got[0] != "mail.ru:443" {
		t.Errorf("direct dialer saw %v, want [mail.ru:443]", got)
	}

	connectAndEcho("example.com:443")
	if _, ok := socksTargets.Load("example.com"); !ok {
		t.Error("CONNECT example.com did not go through the SOCKS server")
	}
	if _, ok := socksTargets.Load("mail.ru"); ok {
		t.Error("CONNECT mail.ru leaked to the SOCKS server")
	}
}
