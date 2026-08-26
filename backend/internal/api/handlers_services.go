package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"minieureka/internal/model"
	"minieureka/internal/service"
)

type registerBody struct {
	InstanceID     string            `json:"instance_id"`
	RegistrationID string            `json:"registration_id"`
	Host           string            `json:"host"`
	Port           int               `json:"port"`
	Protocol       model.Protocol    `json:"protocol"`
	Metadata       map[string]string `json:"metadata"`
	Demo           bool              `json:"demo"`
}

func (s *Server) register(response http.ResponseWriter, request *http.Request) {
	var body registerBody
	if !s.decodeJSON(response, request, &body) {
		return
	}
	if body.Metadata == nil {
		body.Metadata = map[string]string{}
	}
	result, err := s.opts.Service.Register(service.RegisterRequest{
		Service: request.PathValue("service"), InstanceID: body.InstanceID, RegistrationID: body.RegistrationID,
		Host: body.Host, Port: body.Port, Protocol: body.Protocol, Metadata: body.Metadata, Demo: body.Demo,
	})
	if err != nil {
		if !writeDomainError(response, request, err) {
			s.opts.Logger.Error("register instance", "request_id", requestID(request.Context()), "error", err)
			writeError(response, request, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		}
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	response.Header().Set("Location", "/api/v1/services/"+url.PathEscape(result.Record.Service)+"/instances/"+url.PathEscape(result.Record.InstanceID))
	writeData(response, status, result.Record, map[string]any{"duplicate": result.Duplicate, "event_id": result.EventID})
}

type operationBody struct {
	LeaseID     string `json:"lease_id"`
	OperationID string `json:"operation_id"`
}

func (s *Server) heartbeat(response http.ResponseWriter, request *http.Request) {
	var body operationBody
	if !s.decodeJSON(response, request, &body) {
		return
	}
	if body.LeaseID == "" || body.OperationID == "" {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "lease_id and operation_id are required", nil)
		return
	}
	result, err := s.opts.Service.Heartbeat(request.PathValue("service"), request.PathValue("instance"), body.LeaseID, body.OperationID)
	if s.writeServiceError(response, request, err) {
		return
	}
	writeData(response, http.StatusOK, result.Record, map[string]any{"duplicate": result.Duplicate, "event_id": result.EventID})
}

func (s *Server) deregister(response http.ResponseWriter, request *http.Request) {
	leaseID := request.URL.Query().Get("lease_id")
	operationID := request.URL.Query().Get("operation_id")
	if leaseID == "" || operationID == "" {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "lease_id and operation_id are required", nil)
		return
	}
	result, err := s.opts.Service.Deregister(request.PathValue("service"), request.PathValue("instance"), leaseID, operationID, false)
	if s.writeServiceError(response, request, err) {
		return
	}
	if result.EventID != "" {
		response.Header().Set("X-MiniEureka-Event-ID", result.EventID)
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) discover(response http.ResponseWriter, request *http.Request) {
	offset, err := decodeCursor(request.URL.Query().Get("cursor"))
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_cursor", "invalid cursor", nil)
		return
	}
	limit, ok := queryLimit(request, 200, 500)
	if !ok {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "limit must be 1..500", nil)
		return
	}
	allowed := map[model.InstanceStatus]bool{model.StatusActive: true, model.StatusDelayed: true}
	if raw := request.URL.Query().Get("status"); raw != "" {
		allowed = map[model.InstanceStatus]bool{}
		for _, value := range strings.Split(raw, ",") {
			status := model.InstanceStatus(strings.ToUpper(strings.TrimSpace(value)))
			if status != model.StatusActive && status != model.StatusDelayed {
				writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "status may contain ACTIVE and DELAYED", nil)
				return
			}
			allowed[status] = true
		}
	}
	instances := s.opts.Service.Discover(request.PathValue("service"))
	filtered := instances[:0]
	for _, instance := range instances {
		if allowed[instance.Status] {
			filtered = append(filtered, instance)
		}
	}
	page, next, more := paginate(filtered, offset, limit)
	writeData(response, http.StatusOK, page, map[string]any{"next_cursor": next, "has_more": more, "total": len(filtered)})
}

func (s *Server) listServices(response http.ResponseWriter, request *http.Request) {
	offset, err := decodeCursor(request.URL.Query().Get("cursor"))
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_cursor", "invalid cursor", nil)
		return
	}
	limit, ok := queryLimit(request, 100, 500)
	if !ok {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "limit must be 1..500", nil)
		return
	}
	query := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("q")))
	services := s.opts.Service.Services()
	if query != "" {
		filtered := services[:0]
		for _, item := range services {
			if strings.Contains(strings.ToLower(item.Name), query) {
				filtered = append(filtered, item)
			}
		}
		services = filtered
	}
	page, next, more := paginate(services, offset, limit)
	writeData(response, http.StatusOK, page, map[string]any{"next_cursor": next, "has_more": more, "total": len(services)})
}

func (s *Server) writeServiceError(response http.ResponseWriter, request *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeError(response, request, http.StatusNotFound, "not_found", "instance not found", nil)
	case errors.Is(err, service.ErrStaleLease):
		writeError(response, request, http.StatusConflict, "stale_lease", "lease does not match current generation", nil)
	case errors.Is(err, service.ErrNotDemo):
		writeError(response, request, http.StatusNotFound, "demo_disabled", "demo action is not available", nil)
	default:
		if !writeDomainError(response, request, err) {
			s.opts.Logger.Error("service operation", "request_id", requestID(request.Context()), "error", err)
			writeError(response, request, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		}
	}
	return true
}

func queryLimit(request *http.Request, fallback, maximum int) (int, bool) {
	raw := request.URL.Query().Get("limit")
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value >= 1 && value <= maximum
}
