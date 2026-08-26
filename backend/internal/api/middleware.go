package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"minieureka/internal/observe"
)

type contextKey string

const requestIDKey contextKey = "request-id"

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

type middleware struct {
	logger  *slog.Logger
	metrics *observe.Metrics
	limiter *rateLimiter
}

func (m middleware) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		request = request.WithContext(context.WithValue(request.Context(), requestIDKey, newRequestID()))
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-MiniEureka-Request-ID", requestID(request.Context()))
		writer := &statusWriter{ResponseWriter: response, status: http.StatusOK}
		if m.metrics != nil {
			defer m.metrics.ObserveAPI(request.Method, writer.status, time.Since(started))
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				m.logger.Error("http handler panic", "request_id", requestID(request.Context()), "panic", recovered, "stack", string(debug.Stack()))
				if !writer.wroteHeader {
					writeError(writer, request, http.StatusInternalServerError, "internal_error", "internal server error", nil)
				}
			}
		}()
		if m.limiter != nil && !strings.HasPrefix(request.URL.Path, "/internal/") && !m.limiter.Allow(clientIP(request), started) {
			response.Header().Set("Retry-After", "60")
			writeError(writer, request, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded", nil)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	w.status = http.StatusSwitchingProtocols
	w.wroteHeader = true
	return hijacker.Hijack()
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) ReadFrom(reader io.Reader) (int64, error) {
	if from, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return from.ReadFrom(reader)
	}
	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, reader)
}

type rateEntry struct {
	window time.Time
	count  int
}

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	clients map[string]rateEntry
}

func newRateLimiter(limit int) *rateLimiter {
	if limit <= 0 {
		return nil
	}
	return &rateLimiter{limit: limit, clients: make(map[string]rateEntry)}
}

func (l *rateLimiter) Allow(client string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.clients[client]
	if entry.window.IsZero() || now.Sub(entry.window) >= time.Minute {
		entry = rateEntry{window: now, count: 1}
		l.clients[client] = entry
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.clients[client] = entry
	if len(l.clients) > 10000 {
		for key, candidate := range l.clients {
			if now.Sub(candidate.window) >= time.Minute {
				delete(l.clients, key)
			}
		}
	}
	return true
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func newRequestID() string { return "req-" + strings.TrimPrefix(newAPIID(), "id-") }
