package socks5

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	version5 = 0x05

	methodNoAuth       = 0x00
	methodUserPass     = 0x02
	methodNoAcceptable = 0xFF

	cmdConnect = 0x01

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04
)

var replyErrors = map[byte]string{
	0x01: "general SOCKS server failure",
	0x02: "connection not allowed by ruleset",
	0x03: "network unreachable",
	0x04: "host unreachable",
	0x05: "connection refused",
	0x06: "TTL expired",
	0x07: "command not supported",
	0x08: "address type not supported",
}

type Client struct {
	Addr     string
	Username string
	Password string
	Timeout  time.Duration
}

func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("socks5: network %q not supported", network)
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("socks5: invalid address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 0xFFFF {
		return nil, fmt.Errorf("socks5: invalid port %q", portStr)
	}

	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return nil, fmt.Errorf("socks5: dial server %s: %w", c.Addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	if err := c.handshake(conn, host, port); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

func (c *Client) handshake(conn net.Conn, host string, port int) error {
	method := byte(methodNoAuth)
	if c.Username != "" {
		method = methodUserPass
	}
	if _, err := conn.Write([]byte{version5, 1, method}); err != nil {
		return fmt.Errorf("socks5: greeting: %w", err)
	}
	var buf [2]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		return fmt.Errorf("socks5: greeting reply: %w", err)
	}
	if buf[0] != version5 {
		return fmt.Errorf("socks5: server speaks version %d", buf[0])
	}
	switch buf[1] {
	case methodNoAuth:
	case methodUserPass:
		if err := c.authenticate(conn); err != nil {
			return err
		}
	case methodNoAcceptable:
		return errors.New("socks5: server accepts no offered auth method")
	default:
		return fmt.Errorf("socks5: server chose unsupported auth method %#x", buf[1])
	}
	return c.connect(conn, host, port)
}

func (c *Client) authenticate(conn net.Conn) error {
	if len(c.Username) > 255 || len(c.Password) > 255 {
		return errors.New("socks5: username/password too long")
	}
	req := []byte{0x01, byte(len(c.Username))}
	req = append(req, c.Username...)
	req = append(req, byte(len(c.Password)))
	req = append(req, c.Password...)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks5: auth: %w", err)
	}
	var resp [2]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return fmt.Errorf("socks5: auth reply: %w", err)
	}
	if resp[1] != 0x00 {
		return errors.New("socks5: authentication failed")
	}
	return nil
}

func (c *Client) connect(conn net.Conn, host string, port int) error {
	req := []byte{version5, cmdConnect, 0x00}
	switch ip := net.ParseIP(host); {
	case ip == nil:
		if len(host) > 255 {
			return fmt.Errorf("socks5: hostname too long: %q", host)
		}
		req = append(req, atypDomain, byte(len(host)))
		req = append(req, host...)
	case ip.To4() != nil:
		req = append(req, atypIPv4)
		req = append(req, ip.To4()...)
	default:
		req = append(req, atypIPv6)
		req = append(req, ip.To16()...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks5: connect request: %w", err)
	}

	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return fmt.Errorf("socks5: connect reply: %w", err)
	}
	if head[0] != version5 {
		return fmt.Errorf("socks5: bad reply version %d", head[0])
	}
	if head[1] != 0x00 {
		msg, ok := replyErrors[head[1]]
		if !ok {
			msg = fmt.Sprintf("unknown error %#x", head[1])
		}
		return fmt.Errorf("socks5: connect to %s:%d failed: %s", host, port, msg)
	}
	var bindLen int
	switch head[3] {
	case atypIPv4:
		bindLen = 4
	case atypIPv6:
		bindLen = 16
	case atypDomain:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			return fmt.Errorf("socks5: connect reply: %w", err)
		}
		bindLen = int(l[0])
	default:
		return fmt.Errorf("socks5: bad reply address type %#x", head[3])
	}
	if _, err := io.CopyN(io.Discard, conn, int64(bindLen)+2); err != nil {
		return fmt.Errorf("socks5: connect reply: %w", err)
	}
	return nil
}
