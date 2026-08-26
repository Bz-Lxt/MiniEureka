package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"minieureka/internal/events"
	"minieureka/internal/gossip"
	"minieureka/internal/model"
)

func (s *Server) dashboardSnapshot(response http.ResponseWriter, request *http.Request) {
	offset, err := decodeCursor(request.URL.Query().Get("cursor"))
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_cursor", "invalid cursor", nil)
		return
	}
	limit, ok := queryLimit(request, 10000, 10000)
	if !ok {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "limit must be 1..10000", nil)
		return
	}
	instances := filterDashboardInstances(s.opts.Service.DashboardInstances(), request)
	page, next, more := paginate(instances, offset, limit)
	nodes := s.opts.Members.Snapshot()
	edges := s.opts.Members.Edges()
	recent := s.opts.Events.Recent(200)
	summary, revision := dashboardSummary(instances, nodes, recent, s.opts.NodeID)
	data := map[string]any{
		"summary": summary, "instances": page, "nodes": nodes, "edges": edges, "recent_events": recent,
		"capabilities": map[string]bool{"demo_enabled": s.opts.DemoMode, "simulate_offline": s.opts.DemoMode, "network_faults": s.opts.DemoMode},
	}
	writeData(response, http.StatusOK, data, map[string]any{
		"snapshot_revision": revision, "event_cursor": s.opts.Events.Cursor(), "next_cursor": next, "has_more": more,
	})
}

func filterDashboardInstances(instances []model.Instance, request *http.Request) []model.Instance {
	serviceFilter := request.URL.Query().Get("service")
	nodeFilter := request.URL.Query().Get("node")
	statusFilter := strings.ToUpper(request.URL.Query().Get("status"))
	query := strings.ToLower(request.URL.Query().Get("q"))
	result := make([]model.Instance, 0, len(instances))
	for _, instance := range instances {
		if serviceFilter != "" && instance.Service != serviceFilter {
			continue
		}
		if nodeFilter != "" && instance.OriginNodeID != nodeFilter {
			continue
		}
		if statusFilter != "" && string(instance.Status) != statusFilter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(instance.Service+" "+instance.InstanceID), query) {
			continue
		}
		result = append(result, instance)
	}
	return result
}

func dashboardSummary(instances []model.Instance, nodes []model.Member, recent []events.Event, nodeID string) (map[string]any, string) {
	services := make(map[string]struct{})
	active, delayed, evicted := 0, 0, 0
	updated := time.Time{}
	version := model.Version{OriginNodeID: nodeID}
	cutoff := time.Now().Add(-time.Minute)
	gossipHops := 0
	for _, instance := range instances {
		services[instance.Service] = struct{}{}
		switch instance.Status {
		case model.StatusActive:
			active++
		case model.StatusDelayed:
			delayed++
		case model.StatusEvicted:
			evicted++
		}
		if instance.UpdatedAt.After(updated) {
			updated = instance.UpdatedAt
		}
		if model.CompareVersion(instance.Version, version) > 0 {
			version = instance.Version
		}
	}
	for _, event := range recent {
		if event.Type == events.GossipHop && event.OccurredAt.After(cutoff) {
			gossipHops++
		}
	}
	if updated.IsZero() {
		updated = time.Now().UTC()
	}
	return map[string]any{
		"services": len(services), "instances": len(instances), "active": active, "delayed": delayed,
		"evicted": evicted, "nodes": len(nodes), "gossip_rate": float64(gossipHops) / 60, "updated_at": updated,
	}, version.String()
}

func (s *Server) topology(response http.ResponseWriter, _ *http.Request) {
	writeData(response, http.StatusOK, map[string]any{"nodes": s.opts.Members.Snapshot(), "edges": s.opts.Members.Edges()}, map[string]any{"event_cursor": s.opts.Events.Cursor()})
}

func (s *Server) clusterNodes(response http.ResponseWriter, _ *http.Request) {
	writeData(response, http.StatusOK, s.opts.Members.Snapshot(), map[string]any{"event_cursor": s.opts.Events.Cursor()})
}

func (s *Server) gossipEvents(response http.ResponseWriter, request *http.Request) {
	cursor := uint64(0)
	if raw := request.URL.Query().Get("cursor"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeError(response, request, http.StatusBadRequest, "invalid_cursor", "invalid cursor", nil)
			return
		}
		cursor = parsed
	}
	limit, ok := queryLimit(request, 200, 500)
	if !ok {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "limit must be 1..500", nil)
		return
	}
	all, current, err := s.opts.Events.Since(cursor, 2048)
	if errors.Is(err, events.ErrCursorExpired) {
		writeError(response, request, http.StatusConflict, "cursor_expired", "event cursor expired", nil)
		return
	}
	eventID := request.URL.Query().Get("event_id")
	filtered := make([]events.Event, 0, min(limit, len(all)))
	for _, event := range all {
		if eventID != "" && event.EventID != eventID {
			continue
		}
		filtered = append(filtered, event)
		if len(filtered) == limit {
			break
		}
	}
	writeData(response, http.StatusOK, filtered, map[string]any{"event_cursor": current, "has_more": len(filtered) == limit})
}

type demoNetworkBody struct {
	PeerNodeID  string `json:"peer_node_id"`
	DropPercent int    `json:"drop_percent"`
	Blocked     bool   `json:"blocked"`
}

func (s *Server) demoNetwork(response http.ResponseWriter, request *http.Request) {
	if !s.opts.DemoMode || s.opts.Faults == nil {
		writeError(response, request, http.StatusNotFound, "demo_disabled", "demo mode is disabled", nil)
		return
	}
	var body demoNetworkBody
	if !s.decodeJSON(response, request, &body) {
		return
	}
	if body.PeerNodeID == "" || body.DropPercent < 0 || body.DropPercent > 100 {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "peer_node_id is required and drop_percent must be 0..100", nil)
		return
	}
	fault := s.opts.Faults.Set(gossip.Fault{PeerNodeID: body.PeerNodeID, DropPercent: body.DropPercent, Blocked: body.Blocked})
	writeData(response, http.StatusOK, fault, map[string]any{})
}

func (s *Server) demoOffline(response http.ResponseWriter, request *http.Request) {
	if !s.opts.DemoMode || s.opts.DemoOffline == nil {
		writeError(response, request, http.StatusNotFound, "demo_disabled", "demo mode is disabled", nil)
		return
	}
	var body operationBody
	if !s.decodeJSON(response, request, &body) {
		return
	}
	if body.LeaseID == "" || body.OperationID == "" {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "lease_id and operation_id are required", nil)
		return
	}
	result, err := s.opts.DemoOffline(request.PathValue("service"), request.PathValue("instance"), body.LeaseID, body.OperationID)
	if s.writeServiceError(response, request, err) {
		return
	}
	writeData(response, http.StatusAccepted, map[string]any{"event_id": result.EventID, "status": result.Record.Status}, map[string]any{})
}
