package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"minieureka/internal/cluster"
	"minieureka/internal/events"
	"minieureka/internal/gossip"
	"minieureka/internal/observe"
	"minieureka/internal/service"
)

type DemoOfflineFunc func(serviceName, instanceID, leaseID, operationID string) (service.OperationResult, error)

type Options struct {
	NodeID         string
	BootID         string
	DemoMode       bool
	MaxBodyBytes   int64
	RateLimit      int
	AllowedOrigins []string
	Service        *service.Service
	Members        *cluster.Table
	Events         *events.Ring
	Faults         *gossip.Faults
	Metrics        *observe.Metrics
	Readiness      *observe.Readiness
	AntiEntropy    http.Handler
	DemoOffline    DemoOfflineFunc
	Logger         *slog.Logger
}

type Server struct {
	opts    Options
	handler http.Handler
}

func New(options Options) *Server {
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = 64 << 10
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	server := &Server{opts: options}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/services/{service}/instances", server.register)
	mux.HandleFunc("PUT /api/v1/services/{service}/instances/{instance}/heartbeat", server.heartbeat)
	mux.HandleFunc("DELETE /api/v1/services/{service}/instances/{instance}", server.deregister)
	mux.HandleFunc("GET /api/v1/services/{service}/instances", server.discover)
	mux.HandleFunc("GET /api/v1/services", server.listServices)
	mux.HandleFunc("GET /api/v1/dashboard/snapshot", server.dashboardSnapshot)
	mux.HandleFunc("GET /api/v1/cluster/topology", server.topology)
	mux.HandleFunc("GET /api/v1/cluster/nodes", server.clusterNodes)
	mux.HandleFunc("GET /api/v1/gossip/events", server.gossipEvents)
	mux.HandleFunc("GET /api/v1/events/ws", server.websocket)
	mux.HandleFunc("POST /api/v1/demo/services/{service}/instances/{instance}/offline", server.demoOffline)
	mux.HandleFunc("PUT /api/v1/demo/network", server.demoNetwork)
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	if options.Metrics != nil {
		mux.Handle("GET /metrics", options.Metrics.Handler())
	}
	if options.AntiEntropy != nil {
		mux.Handle("POST /internal/v1/anti-entropy", options.AntiEntropy)
	}
	middle := middleware{logger: options.Logger, metrics: options.Metrics, limiter: newRateLimiter(options.RateLimit)}
	server.handler = middle.wrap(mux)
	return server
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"status": "ok", "node_id": s.opts.NodeID})
}

func (s *Server) ready(response http.ResponseWriter, request *http.Request) {
	checks := s.opts.Readiness.Checks()
	if !s.opts.Readiness.Ready() {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "node_id": s.opts.NodeID, "checks": checks})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"status": "ready", "node_id": s.opts.NodeID, "checks": checks})
}

func (s *Server) decodeJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, s.opts.MaxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maximum *http.MaxBytesError
		if errors.As(err, &maximum) {
			writeError(response, request, http.StatusRequestEntityTooLarge, "body_too_large", "request body is too large", nil)
		} else {
			writeError(response, request, http.StatusBadRequest, "invalid_json", "invalid JSON request body", nil)
		}
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(response, request, http.StatusBadRequest, "invalid_json", "request body must contain one JSON value", nil)
		return false
	}
	return true
}

func newAPIID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "id-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "id-" + hex.EncodeToString(value[:])
}
