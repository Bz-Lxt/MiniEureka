package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"minieureka/internal/model"
)

type fieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorBody struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Details   []fieldError `json:"details,omitempty"`
	RequestID string       `json:"request_id"`
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeData(response http.ResponseWriter, status int, data, meta any) {
	if meta == nil {
		meta = map[string]any{}
	}
	writeJSON(response, status, map[string]any{"data": data, "meta": meta})
}

func writeError(response http.ResponseWriter, request *http.Request, status int, code, message string, details []fieldError) {
	if details == nil {
		details = []fieldError{}
	}
	writeJSON(response, status, map[string]any{"error": errorBody{Code: code, Message: message, Details: details, RequestID: requestID(request.Context())}})
}

func writeDomainError(response http.ResponseWriter, request *http.Request, err error) bool {
	var validation *model.ValidationError
	if errors.As(err, &validation) {
		writeError(response, request, http.StatusUnprocessableEntity, "validation_error", "request validation failed", []fieldError{{Field: validation.Field, Code: validation.Code, Message: validation.Message}})
		return true
	}
	return false
}
