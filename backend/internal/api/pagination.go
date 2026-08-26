package api

import (
	"encoding/base64"
	"errors"
	"strconv"
)

var errInvalidCursor = errors.New("invalid cursor")

func encodeCursor(offset int) *string {
	if offset < 0 {
		return nil
	}
	value := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
	return &value
}

func decodeCursor(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, errInvalidCursor
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, errInvalidCursor
	}
	return offset, nil
}

func paginate[T any](values []T, offset, limit int) ([]T, *string, bool) {
	if offset >= len(values) {
		return []T{}, nil, false
	}
	end := min(offset+limit, len(values))
	page := append([]T(nil), values[offset:end]...)
	if end < len(values) {
		return page, encodeCursor(end), true
	}
	return page, nil, false
}
