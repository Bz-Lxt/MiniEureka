package model

type MutationKind string

const (
	MutationRegister    MutationKind = "REGISTER"
	MutationHeartbeat   MutationKind = "HEARTBEAT"
	MutationDelayed     MutationKind = "DELAYED"
	MutationTTLExpire   MutationKind = "TTL_EXPIRE"
	MutationDeregister  MutationKind = "DEREGISTER"
	MutationDemoOffline MutationKind = "DEMO_OFFLINE"
)

func (k MutationKind) Valid() bool {
	switch k {
	case MutationRegister, MutationHeartbeat, MutationDelayed, MutationTTLExpire,
		MutationDeregister, MutationDemoOffline:
		return true
	default:
		return false
	}
}

func (k MutationKind) ExplicitlyTerminal() bool {
	return k == MutationDeregister || k == MutationDemoOffline
}

func (k MutationKind) Priority() int {
	switch k {
	case MutationDeregister, MutationDemoOffline:
		return 4
	case MutationTTLExpire:
		return 3
	case MutationHeartbeat:
		return 2
	case MutationRegister:
		return 1
	case MutationDelayed:
		return 0
	default:
		return -1
	}
}

type Mutation struct {
	Kind               MutationKind `json:"kind"`
	Record             Instance     `json:"record"`
	EventID            string       `json:"event_id"`
	OperationID        string       `json:"operation_id,omitempty"`
	RemainingTTLMillis int64        `json:"remaining_ttl_ms,omitempty"`
}

func (m Mutation) Validate() error {
	if !m.Kind.Valid() {
		return &ValidationError{Field: "kind", Code: "invalid_enum", Message: "unknown mutation kind"}
	}
	if err := validateOpaqueID("event_id", m.EventID, MaxEventIDLength, true); err != nil {
		return err
	}
	if err := validateOpaqueID("operation_id", m.OperationID, MaxOperationIDLength, false); err != nil {
		return err
	}
	if m.RemainingTTLMillis < 0 {
		return &ValidationError{Field: "remaining_ttl_ms", Code: "out_of_range", Message: "must not be negative"}
	}
	if err := m.Record.Validate(); err != nil {
		return err
	}
	if err := validateMutationState(m); err != nil {
		return err
	}
	return nil
}

func validateMutationState(m Mutation) error {
	wantStatus := StatusActive
	wantReason := ReasonRegistered
	switch m.Kind {
	case MutationRegister:
		wantReason = ReasonRegistered
	case MutationHeartbeat:
		wantReason = ReasonHeartbeatOK
	case MutationDelayed:
		wantStatus, wantReason = StatusDelayed, ReasonHeartbeatDelayed
	case MutationTTLExpire:
		wantStatus, wantReason = StatusEvicted, ReasonTTLExpired
	case MutationDeregister:
		wantStatus, wantReason = StatusEvicted, ReasonDeregistered
	case MutationDemoOffline:
		wantStatus, wantReason = StatusEvicted, ReasonDemoOffline
	}
	if m.Record.Status != wantStatus || m.Record.StatusReason != wantReason {
		return &ValidationError{Field: "record.status", Code: "invalid_state", Message: "does not match mutation kind"}
	}
	return nil
}
