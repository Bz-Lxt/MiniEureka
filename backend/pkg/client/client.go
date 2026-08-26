package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"minieureka/internal/model"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Mini Eureka base URL %q", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}, nil
}

type RegisterRequest struct {
	InstanceID     string            `json:"instance_id"`
	RegistrationID string            `json:"registration_id"`
	Host           string            `json:"host"`
	Port           int               `json:"port"`
	Protocol       model.Protocol    `json:"protocol"`
	Metadata       map[string]string `json:"metadata"`
	Demo           bool              `json:"demo"`
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Mini Eureka API %d %s: %s", e.Status, e.Code, e.Message)
}

func (c *Client) Register(ctx context.Context, service string, request RegisterRequest) (model.Instance, error) {
	var response struct {
		Data model.Instance `json:"data"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/services/"+url.PathEscape(service)+"/instances", request, &response)
	return response.Data, err
}

func (c *Client) Heartbeat(ctx context.Context, instance model.Instance, operationID string) (model.Instance, error) {
	var response struct {
		Data model.Instance `json:"data"`
	}
	path := "/api/v1/services/" + url.PathEscape(instance.Service) + "/instances/" + url.PathEscape(instance.InstanceID) + "/heartbeat"
	err := c.doJSON(ctx, http.MethodPut, path, map[string]string{"lease_id": instance.LeaseID, "operation_id": operationID}, &response)
	return response.Data, err
}

func (c *Client) Discover(ctx context.Context, service string) ([]model.Instance, error) {
	var response struct {
		Data []model.Instance `json:"data"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/services/"+url.PathEscape(service)+"/instances", nil, &response)
	if response.Data == nil {
		response.Data = []model.Instance{}
	}
	return response.Data, err
}

func (c *Client) doJSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	requestContext := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		requestContext, cancel = context.WithDeadline(requestContext, deadline)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(requestContext, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 4<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(limited).Decode(&payload)
		return &APIError{Status: response.StatusCode, Code: payload.Error.Code, Message: payload.Error.Message}
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(limited).Decode(output); err != nil {
		return fmt.Errorf("decode Mini Eureka response: %w", err)
	}
	return nil
}
