package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"proxify/internal/router"
)

type Server struct {
	router      *router.Router
	log         *slog.Logger
	dialTimeout time.Duration
	transport   *http.Transport
}

func New(r *router.Router, log *slog.Logger, dialTimeout time.Duration) *Server {
	s := &Server{router: r, log: log, dialTimeout: dialTimeout}
	s.transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, dialer := r.RouteFor(addr)
			ctx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()
			return dialer.DialContext(ctx, network, addr)
		},
		Proxy:                 nil,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodConnect {
		s.handleConnect(w, req)
		return
	}
	if !req.URL.IsAbs() {
		http.Error(w, "proxify is an HTTP proxy; configure it as one", http.StatusBadRequest)
		return
	}
	s.handleHTTP(w, req)
}

func (s *Server) handleConnect(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	target := req.Host
	if _, _, err := net.SplitHostPort(target); err != nil {
		target = net.JoinHostPort(target, "443")
	}
	route, dialer := s.router.RouteFor(target)
	log := s.log.With("host", target, "route", route)

	dialCtx, cancel := context.WithTimeout(req.Context(), s.dialTimeout)
	upstream, err := dialer.DialContext(dialCtx, "tcp", target)
	cancel()
	if err != nil {
		log.Error("connect failed", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Error("connect failed", "err", "response writer does not support hijacking")
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	client, clientRW, err := hijacker.Hijack()
	if err != nil {
		log.Error("hijack failed", "err", err)
		return
	}
	defer client.Close()
	client.SetDeadline(time.Time{})

	if _, err := clientRW.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := clientRW.Flush(); err != nil {
		return
	}

	bytesUp, bytesDown := tunnel(client, clientRW.Reader, upstream)
	log.Info("tunnel closed",
		"duration", time.Since(start).Round(time.Millisecond),
		"sent", bytesUp, "received", bytesDown)
}

func tunnel(client net.Conn, clientR io.Reader, upstream net.Conn) (up, down int64) {
	done := make(chan struct{})
	go func() {
		up, _ = io.Copy(upstream, clientR)
		closeWrite(upstream)
		close(done)
	}()
	down, _ = io.Copy(client, upstream)
	closeWrite(client)
	<-done
	return up, down
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	} else {
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
	}
}

func (s *Server) handleHTTP(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	route, _ := s.router.RouteFor(req.URL.Host)
	log := s.log.With("host", req.URL.Host, "route", route, "method", req.Method)

	out := req.Clone(req.Context())
	out.RequestURI = ""
	out.Close = false
	removeHopByHop(out.Header)

	resp, err := s.transport.RoundTrip(out)
	if err != nil {
		log.Error("request failed", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHop(resp.Header)
	header := w.Header()
	for k, vv := range resp.Header {
		header[k] = vv
	}
	w.WriteHeader(resp.StatusCode)
	written, _ := io.Copy(w, resp.Body)
	log.Info("request done",
		"status", resp.StatusCode,
		"duration", time.Since(start).Round(time.Millisecond),
		"bytes", written)
}

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func removeHopByHop(h http.Header) {
	for _, name := range h.Values("Connection") {
		for _, field := range strings.Split(name, ",") {
			if field = strings.TrimSpace(field); field != "" {
				h.Del(field)
			}
		}
	}
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
}

func ListenAndServe(ctx context.Context, addr string, s *Server) error {
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}
	errc := make(chan error, 1)
	go func() { errc <- httpServer.ListenAndServe() }()
	s.log.Info("listening", "addr", addr)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
