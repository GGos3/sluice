package proxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ggos3/sluice/internal/logger"
)

func (h *Handler) handleConnect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	target := r.Host

	if !h.authorized(r) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="proxy"`)
		h.writeErrorResponse(w, r, start, target, 0, http.StatusProxyAuthRequired, false, "proxy_auth_required")
		return
	}

	if !h.isAllowed(target) {
		h.writeErrorResponse(w, r, start, target, 0, http.StatusForbidden, false, "domain_not_allowed")
		return
	}

	targetConn, err := net.DialTimeout("tcp", target, 30*time.Second)
	if err != nil {
		h.writeErrorResponse(w, r, start, target, 0, http.StatusBadGateway, false, "target_dial_failed")
		return
	}
	defer targetConn.Close()

	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		h.writeErrorResponse(w, r, start, target, 0, http.StatusInternalServerError, false, "hijack_failed")
		return
	}
	defer conn.Close()

	if rw != nil && rw.Writer != nil {
		if err := rw.Writer.Flush(); err != nil {
			h.logAccess(r, logger.AccessLogEntry{
				SourceIP:   sourceIP(r.RemoteAddr),
				Method:     r.Method,
				Domain:     target,
				Status:     http.StatusInternalServerError,
				BytesIn:    0,
				BytesOut:   0,
				DurationMs: durationMillis(start),
				Allowed:    false,
				Reason:     "flush_failed",
			})
			return
		}
	}

	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		h.logAccess(r, logger.AccessLogEntry{
			SourceIP:   sourceIP(r.RemoteAddr),
			Method:     r.Method,
			Domain:     target,
			Status:     http.StatusInternalServerError,
			BytesIn:    0,
			BytesOut:   0,
			DurationMs: durationMillis(start),
			Allowed:    false,
			Reason:     "connect_response_failed",
		})
		return
	}

	bytesIn, bytesOut := bidirectionalCopy(targetConn, conn)
	h.logAccess(r, logger.AccessLogEntry{
		SourceIP:   sourceIP(r.RemoteAddr),
		Method:     r.Method,
		Domain:     target,
		Status:     http.StatusOK,
		BytesIn:    bytesIn,
		BytesOut:   bytesOut,
		DurationMs: durationMillis(start),
		Allowed:    true,
		Reason:     "ok",
	})
}

func bidirectionalCopy(dst, src net.Conn) (int64, int64) {
	type result struct {
		bytes    int64
		direction int
	}

	results := make(chan result, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	copyOne := func(direction int, writer net.Conn, reader net.Conn) {
		defer wg.Done()
		n, err := io.Copy(writer, reader)
		closeWrite(writer)
		if err != nil && !errors.Is(err, net.ErrClosed) {
			results <- result{bytes: n, direction: direction}
			return
		}
		results <- result{bytes: n, direction: direction}
	}

	go copyOne(0, dst, src)
	go copyOne(1, src, dst)

	go func() {
		wg.Wait()
		close(results)
	}()

	var srcToDst int64
	var dstToSrc int64
	for res := range results {
		if res.direction == 0 {
			srcToDst = res.bytes
		} else {
			dstToSrc = res.bytes
		}
	}

	return srcToDst, dstToSrc
}

func closeWrite(conn net.Conn) {
	type closeWriter interface {
		CloseWrite() error
	}
	if cw, ok := conn.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = conn.Close()
}

