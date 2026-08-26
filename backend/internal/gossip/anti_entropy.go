package gossip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"minieureka/internal/model"
	"minieureka/internal/registry"
)

const antiEntropyPageSize = 200

func (e *Engine) AntiEntropyHandler(maxBodyBytes int64) http.Handler {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 4 << 20
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", http.MethodPost)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := readLimited(request.Body, maxBodyBytes)
		if errors.Is(err, errBodyTooLarge) {
			http.Error(response, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			http.Error(response, "invalid body", http.StatusBadRequest)
			return
		}
		if err := e.auth.VerifyHTTPRequest(request, body); err != nil {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		nodeID := request.Header.Get("X-MiniEureka-Node-ID")
		if _, ok := e.members.Get(nodeID); !ok {
			http.Error(response, "unknown member", http.StatusForbidden)
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var syncRequest AntiEntropyRequest
		if err := decoder.Decode(&syncRequest); err != nil || syncRequest.Cursor < 0 {
			http.Error(response, "invalid request", http.StatusBadRequest)
			return
		}
		syncResponse := e.buildAntiEntropyResponse(syncRequest)
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(response).Encode(syncResponse)
	})
}

type syncItem struct {
	mutation *model.Mutation
	fence    *registry.Fence
}

func (e *Engine) buildAntiEntropyResponse(request AntiEntropyRequest) AntiEntropyResponse {
	remote := make(map[int]string, len(request.Digests))
	for _, digest := range request.Digests {
		remote[digest.Shard] = digest.SHA256
	}
	localDigests := e.registry.Digests()
	mismatched := make(map[int]bool)
	items := make([]syncItem, 0)
	for _, digest := range localDigests {
		if hash, ok := remote[digest.Shard]; ok && hash == digest.SHA256 {
			continue
		}
		mismatched[digest.Shard] = true
		mutations, _ := e.registry.MutationsForShard(digest.Shard)
		for _, mutation := range mutations {
			copyMutation := mutation
			items = append(items, syncItem{mutation: &copyMutation})
		}
	}
	for _, fence := range e.registry.Fences() {
		if !mismatched[e.registry.ShardIndex(fence.Key.Service)] {
			continue
		}
		copyFence := fence
		items = append(items, syncItem{fence: &copyFence})
	}
	start := request.Cursor
	if start > len(items) {
		start = len(items)
	}
	end := min(start+antiEntropyPageSize, len(items))
	result := AntiEntropyResponse{Mutations: []model.Mutation{}, Fences: []registry.Fence{}, Digests: localDigests}
	for _, item := range items[start:end] {
		if item.mutation != nil {
			result.Mutations = append(result.Mutations, *item.mutation)
		} else if item.fence != nil {
			result.Fences = append(result.Fences, *item.fence)
		}
	}
	if end < len(items) {
		next := end
		result.NextCursor = &next
	}
	return result
}

func (e *Engine) SyncPeer(ctx context.Context, member model.Member) error {
	endpoint, err := antiEntropyURL(member.HTTPAddress)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 4 * time.Second}
	cursor := 0
	// Keep the initiating summary stable across pages. Recomputing it after each
	// applied page can shrink the server's result set and make an offset cursor
	// skip records from later shards.
	initialDigests := e.registry.Digests()
	for {
		requestPayload := AntiEntropyRequest{Digests: initialDigests, Cursor: cursor}
		body, err := json.Marshal(requestPayload)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		e.auth.SignHTTPRequest(request, e.config.NodeID, body)
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("anti-entropy request: %w", err)
		}
		responseBody, readErr := readLimited(response.Body, 4<<20)
		_ = response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read anti-entropy response: %w", readErr)
		}
		if response.StatusCode != http.StatusOK {
			if cursor > 0 {
				break
			}
			return fmt.Errorf("anti-entropy response status %d", response.StatusCode)
		}
		var payload AntiEntropyResponse
		if err := json.Unmarshal(responseBody, &payload); err != nil {
			return fmt.Errorf("decode anti-entropy response: %w", err)
		}
		for _, mutation := range payload.Mutations {
			if _, err := e.applier.ApplyRemote(mutation); err != nil {
				e.logger.Debug("anti-entropy mutation rejected", "event_id", mutation.EventID, "error", err)
			}
		}
		for _, fence := range payload.Fences {
			e.registry.ApplyFence(fence)
		}
		if payload.NextCursor == nil {
			break
		}
		if *payload.NextCursor <= cursor {
			return errors.New("anti-entropy cursor did not advance")
		}
		cursor = *payload.NextCursor
	}
	return nil
}

func antiEntropyURL(httpAddress string) (string, error) {
	parsed, err := url.Parse(httpAddress)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid peer HTTP address %q", httpAddress)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/internal/v1/anti-entropy"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

var errBodyTooLarge = errors.New("body too large")

func readLimited(reader io.Reader, maximum int64) ([]byte, error) {
	limited := io.LimitReader(reader, maximum+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errBodyTooLarge
	}
	return data, nil
}
