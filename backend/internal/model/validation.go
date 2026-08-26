package model

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxServiceLength      = 128
	MaxInstanceIDLength   = 128
	MaxRegistrationLength = 128
	MaxLeaseIDLength      = 128
	MaxOperationIDLength  = 128
	MaxEventIDLength      = 192
	MaxNodeIDLength       = 128
	MaxHostLength         = 255
	MaxMetadataItems      = 32
	MaxMetadataKeyLength  = 64
	MaxMetadataValueLen   = 256
)

// ValidationError is intentionally transport-neutral. HTTP handlers may map
// it to the documented validation_error response without exposing internals.
type ValidationError struct {
	Field   string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func validateIdentifier(field, value string, max int) error {
	if value == "" {
		return &ValidationError{Field: field, Code: "required", Message: "must not be empty"}
	}
	if !utf8.ValidString(value) {
		return &ValidationError{Field: field, Code: "invalid_utf8", Message: "must be valid UTF-8"}
	}
	if utf8.RuneCountInString(value) > max {
		return &ValidationError{Field: field, Code: "too_long", Message: fmt.Sprintf("must be at most %d characters", max)}
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '/' || r == '\\' {
			return &ValidationError{Field: field, Code: "invalid_format", Message: "must not contain whitespace, control characters, or path separators"}
		}
	}
	return nil
}

func validateOpaqueID(field, value string, max int, required bool) error {
	if value == "" && !required {
		return nil
	}
	return validateIdentifier(field, value, max)
}

func validateHost(host string) error {
	if host == "" {
		return &ValidationError{Field: "host", Code: "required", Message: "must not be empty"}
	}
	if !utf8.ValidString(host) || utf8.RuneCountInString(host) > MaxHostLength {
		return &ValidationError{Field: "host", Code: "invalid_length", Message: "must be valid UTF-8 and at most 255 characters"}
	}
	if strings.TrimSpace(host) != host {
		return &ValidationError{Field: "host", Code: "invalid_format", Message: "must not have surrounding whitespace"}
	}
	for _, r := range host {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '/' || r == '\\' {
			return &ValidationError{Field: "host", Code: "invalid_format", Message: "must be a host name or IP address without a port"}
		}
	}
	return nil
}
