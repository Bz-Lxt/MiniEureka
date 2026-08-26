package model

import (
	"fmt"
	"maps"
	"reflect"
	"time"
	"unicode"
	"unicode/utf8"
)

type InstanceStatus string

const (
	StatusActive  InstanceStatus = "ACTIVE"
	StatusDelayed InstanceStatus = "DELAYED"
	StatusEvicted InstanceStatus = "EVICTED"
)

func (s InstanceStatus) Valid() bool {
	switch s {
	case StatusActive, StatusDelayed, StatusEvicted:
		return true
	default:
		return false
	}
}

type StatusReason string

const (
	ReasonRegistered       StatusReason = "REGISTERED"
	ReasonHeartbeatOK      StatusReason = "HEARTBEAT_OK"
	ReasonHeartbeatDelayed StatusReason = "HEARTBEAT_DELAYED"
	ReasonTTLExpired       StatusReason = "TTL_EXPIRED"
	ReasonDeregistered     StatusReason = "DEREGISTERED"
	ReasonDemoOffline      StatusReason = "DEMO_OFFLINE"
)

func (r StatusReason) Valid() bool {
	switch r {
	case ReasonRegistered, ReasonHeartbeatOK, ReasonHeartbeatDelayed,
		ReasonTTLExpired, ReasonDeregistered, ReasonDemoOffline:
		return true
	default:
		return false
	}
}

func (r StatusReason) Terminal() bool {
	return r == ReasonDeregistered || r == ReasonDemoOffline
}

type Protocol string

const (
	ProtocolHTTP  Protocol = "http"
	ProtocolHTTPS Protocol = "https"
	ProtocolGRPC  Protocol = "grpc"
	ProtocolTCP   Protocol = "tcp"
)

func (p Protocol) Valid() bool {
	switch p {
	case ProtocolHTTP, ProtocolHTTPS, ProtocolGRPC, ProtocolTCP:
		return true
	default:
		return false
	}
}

type Key struct {
	Service    string `json:"service"`
	InstanceID string `json:"instance_id"`
}

func (k Key) String() string { return k.Service + "/" + k.InstanceID }

func (k Key) Validate() error {
	if err := validateIdentifier("service", k.Service, MaxServiceLength); err != nil {
		return err
	}
	return validateIdentifier("instance_id", k.InstanceID, MaxInstanceIDLength)
}

// Instance is an immutable-at-package-boundary registry value. Registry read
// APIs return clones because Metadata is mutable in Go.
type Instance struct {
	Service            string            `json:"service"`
	InstanceID         string            `json:"instance_id"`
	RegistrationID     string            `json:"registration_id"`
	Host               string            `json:"host"`
	Port               int               `json:"port"`
	Protocol           Protocol          `json:"protocol"`
	Metadata           map[string]string `json:"metadata"`
	Status             InstanceStatus    `json:"status"`
	StatusReason       StatusReason      `json:"status_reason"`
	Generation         uint64            `json:"generation"`
	LeaseID            string            `json:"lease_id"`
	LeaseEpoch         Version           `json:"lease_epoch"`
	Version            Version           `json:"version"`
	OriginNodeID       string            `json:"origin_node_id"`
	RegisteredAt       time.Time         `json:"registered_at"`
	LastHeartbeatAt    time.Time         `json:"last_heartbeat_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	EvictedAt          *time.Time        `json:"evicted_at"`
	Demo               bool              `json:"demo"`
	LeaseDeadline      time.Time         `json:"-"`
	LastRemainingTTLMs int64             `json:"-"`
}

// InstanceRecord is retained as a descriptive alias for registry-facing code.
type InstanceRecord = Instance

func (i Instance) Key() Key {
	return Key{Service: i.Service, InstanceID: i.InstanceID}
}

func (i Instance) Discoverable() bool {
	return i.Status == StatusActive || i.Status == StatusDelayed
}

func (i Instance) ExplicitlyTerminal() bool {
	return i.Status == StatusEvicted && i.StatusReason.Terminal()
}

func (i Instance) Clone() Instance {
	result := i
	if i.Metadata == nil {
		result.Metadata = map[string]string{}
	} else {
		result.Metadata = maps.Clone(i.Metadata)
	}
	if i.EvictedAt != nil {
		t := *i.EvictedAt
		result.EvictedAt = &t
	}
	return result
}

func (i Instance) Equal(other Instance) bool {
	return reflect.DeepEqual(i, other)
}

func (i Instance) Validate() error {
	if err := i.Key().Validate(); err != nil {
		return err
	}
	if err := validateOpaqueID("registration_id", i.RegistrationID, MaxRegistrationLength, true); err != nil {
		return err
	}
	if err := validateHost(i.Host); err != nil {
		return err
	}
	if i.Port < 1 || i.Port > 65535 {
		return &ValidationError{Field: "port", Code: "out_of_range", Message: "must be 1..65535"}
	}
	if !i.Protocol.Valid() {
		return &ValidationError{Field: "protocol", Code: "invalid_enum", Message: "must be http, https, grpc, or tcp"}
	}
	if len(i.Metadata) > MaxMetadataItems {
		return &ValidationError{Field: "metadata", Code: "too_many_items", Message: "must contain at most 32 items"}
	}
	for key, value := range i.Metadata {
		if key == "" || !utf8.ValidString(key) || utf8.RuneCountInString(key) > MaxMetadataKeyLength {
			return &ValidationError{Field: "metadata", Code: "invalid_key", Message: "keys must be 1..64 valid UTF-8 characters"}
		}
		for _, r := range key {
			if unicode.IsControl(r) {
				return &ValidationError{Field: "metadata", Code: "invalid_key", Message: "keys must not contain control characters"}
			}
		}
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxMetadataValueLen {
			return &ValidationError{Field: "metadata", Code: "invalid_value", Message: "values must be at most 256 valid UTF-8 characters"}
		}
	}
	if !i.Status.Valid() {
		return &ValidationError{Field: "status", Code: "invalid_enum", Message: "must be ACTIVE, DELAYED, or EVICTED"}
	}
	if !i.StatusReason.Valid() {
		return &ValidationError{Field: "status_reason", Code: "invalid_enum", Message: "unknown status reason"}
	}
	if i.Generation == 0 {
		return &ValidationError{Field: "generation", Code: "out_of_range", Message: "must be positive"}
	}
	if err := validateOpaqueID("lease_id", i.LeaseID, MaxLeaseIDLength, true); err != nil {
		return err
	}
	if err := i.LeaseEpoch.Validate(); err != nil {
		return fmt.Errorf("lease_epoch: %w", err)
	}
	if err := i.Version.Validate(); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if err := validateIdentifier("origin_node_id", i.OriginNodeID, MaxNodeIDLength); err != nil {
		return err
	}
	if i.RegisteredAt.IsZero() || i.UpdatedAt.IsZero() {
		return &ValidationError{Field: "timestamps", Code: "required", Message: "registered_at and updated_at are required"}
	}
	if i.LastHeartbeatAt.IsZero() {
		return &ValidationError{Field: "last_heartbeat_at", Code: "required", Message: "must not be zero"}
	}
	if i.Status == StatusEvicted && i.EvictedAt == nil {
		return &ValidationError{Field: "evicted_at", Code: "required", Message: "is required for EVICTED status"}
	}
	if i.Status != StatusEvicted && i.EvictedAt != nil {
		return &ValidationError{Field: "evicted_at", Code: "invalid_state", Message: "must be null unless status is EVICTED"}
	}
	return nil
}
