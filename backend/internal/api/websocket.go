package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"minieureka/internal/events"
)

func (s *Server) websocket(response http.ResponseWriter, request *http.Request) {
	cursor := uint64(0)
	if raw := request.URL.Query().Get("cursor"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeError(response, request, http.StatusBadRequest, "invalid_cursor", "invalid cursor", nil)
			return
		}
		cursor = parsed
	}
	stream, cancel, subscribeErr := s.opts.Events.Subscribe(cursor, 256)
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
		CheckOrigin:      func(r *http.Request) bool { return originAllowed(r, s.opts.AllowedOrigins) },
	}
	connection, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return
	}
	defer connection.Close()
	if errors.Is(subscribeErr, events.ErrCursorExpired) {
		_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = connection.WriteJSON(events.Event{Seq: s.opts.Events.Cursor(), SchemaVersion: events.SchemaVersion, StreamNodeID: s.opts.NodeID, StreamBootID: s.opts.BootID, Type: events.ResyncRequired, EventID: newAPIID(), OriginNodeID: s.opts.NodeID, OccurredAt: time.Now().UTC(), Payload: events.Payload(map[string]string{"reason": "cursor_expired"})})
		return
	}
	if subscribeErr != nil {
		return
	}
	defer cancel()
	connection.SetReadLimit(4096)
	_ = connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}()
	connected := events.Event{Seq: cursor, SchemaVersion: events.SchemaVersion, StreamNodeID: s.opts.NodeID, StreamBootID: s.opts.BootID, Type: events.Connected, EventID: newAPIID(), OriginNodeID: s.opts.NodeID, OccurredAt: time.Now().UTC(), Payload: events.Payload(map[string]any{"cursor": s.opts.Events.Cursor()})}
	if err := writeWebSocketEvent(connection, connected); err != nil {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-readDone:
			return
		case event, ok := <-stream:
			if !ok {
				return
			}
			if err := writeWebSocketEvent(connection, event); err != nil {
				return
			}
		case <-ping.C:
			_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

func writeWebSocketEvent(connection *websocket.Conn, event events.Event) error {
	if err := connection.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return connection.WriteJSON(event)
}

func originAllowed(request *http.Request, allowed []string) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, request.Host) {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimRight(candidate, "/"), strings.TrimRight(origin, "/")) {
			return true
		}
	}
	hostname := parsed.Hostname()
	requestHost, _, splitErr := net.SplitHostPort(request.Host)
	if splitErr != nil {
		requestHost = request.Host
	}
	return isLoopbackName(hostname) && isLoopbackName(requestHost)
}

func isLoopbackName(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}
