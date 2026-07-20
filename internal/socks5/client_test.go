package socks5

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

type fakeServer struct {
	ln         net.Listener
	wantUser   string
	wantPass   string
	targetHost chan string
}

func startFakeServer(t *testing.T, user, pass string) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{ln: ln, wantUser: user, wantPass: pass, targetHost: make(chan string, 1)}
	t.Cleanup(func() { ln.Close() })
	go s.serve(t)
	return s
}

func (s *fakeServer) serve(t *testing.T) {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Error(err)
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		t.Error(err)
		return
	}
	if s.wantUser != "" {
		conn.Write([]byte{0x05, methodUserPass})
		var b [2]byte
		io.ReadFull(conn, b[:])
		user := make([]byte, b[1])
		io.ReadFull(conn, user)
		var pl [1]byte
		io.ReadFull(conn, pl[:])
		pass := make([]byte, pl[0])
		io.ReadFull(conn, pass)
		if string(user) != s.wantUser || string(pass) != s.wantPass {
			conn.Write([]byte{0x01, 0x01})
			return
		}
		conn.Write([]byte{0x01, 0x00})
	} else {
		conn.Write([]byte{0x05, methodNoAuth})
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		t.Error(err)
		return
	}
	var host string
	switch req[3] {
	case atypDomain:
		var l [1]byte
		io.ReadFull(conn, l[:])
		name := make([]byte, l[0])
		io.ReadFull(conn, name)
		host = string(name)
	case atypIPv4:
		ip := make([]byte, 4)
		io.ReadFull(conn, ip)
		host = net.IP(ip).String()
	case atypIPv6:
		ip := make([]byte, 16)
		io.ReadFull(conn, ip)
		host = net.IP(ip).String()
	}
	var port [2]byte
	io.ReadFull(conn, port[:])
	s.targetHost <- host

	reply := []byte{0x05, 0x00, 0x00, atypIPv4, 127, 0, 0, 1}
	reply = binary.BigEndian.AppendUint16(reply, 1080)
	conn.Write(reply)
	io.Copy(conn, conn)
}

func TestDialSendsHostnameAsDomain(t *testing.T) {
	srv := startFakeServer(t, "", "")
	c := &Client{Addr: srv.ln.Addr().String(), Timeout: 5 * time.Second}

	conn, err := c.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if got := <-srv.targetHost; got != "example.com" {
		t.Errorf("server saw target %q, want %q (hostname must not be resolved locally)", got, "example.com")
	}

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Errorf("echo = %q, want %q", buf, "ping")
	}
}

func TestDialWithAuth(t *testing.T) {
	srv := startFakeServer(t, "user", "secret")
	c := &Client{Addr: srv.ln.Addr().String(), Username: "user", Password: "secret", Timeout: 5 * time.Second}
	conn, err := c.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestDialRejectsUDP(t *testing.T) {
	c := &Client{Addr: "127.0.0.1:1"}
	if _, err := c.DialContext(context.Background(), "udp", "example.com:53"); err == nil {
		t.Error("expected error for udp network")
	}
}
