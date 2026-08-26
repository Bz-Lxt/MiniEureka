package model

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a Hybrid Logical Clock value. OriginNodeID is part of the value
// so independently generated versions still have a deterministic total order.
type Version struct {
	PhysicalMillis int64  `json:"physical_ms"`
	Logical        uint64 `json:"logical"`
	OriginNodeID   string `json:"origin_node_id"`
}

func (v Version) IsZero() bool {
	return v.PhysicalMillis == 0 && v.Logical == 0 && v.OriginNodeID == ""
}

func (v Version) String() string {
	return strconv.FormatInt(v.PhysicalMillis, 10) + "-" +
		strconv.FormatUint(v.Logical, 10) + "-" + v.OriginNodeID
}

func (v Version) Validate() error {
	if v.PhysicalMillis <= 0 {
		return &ValidationError{Field: "physical_ms", Code: "out_of_range", Message: "must be positive"}
	}
	if err := validateIdentifier("origin_node_id", v.OriginNodeID, MaxNodeIDLength); err != nil {
		return err
	}
	return nil
}

// CompareVersion returns -1, 0 or 1 using the frozen total ordering:
// physical time, logical counter, then origin node ID.
func CompareVersion(a, b Version) int {
	if a.PhysicalMillis < b.PhysicalMillis {
		return -1
	}
	if a.PhysicalMillis > b.PhysicalMillis {
		return 1
	}
	if a.Logical < b.Logical {
		return -1
	}
	if a.Logical > b.Logical {
		return 1
	}
	return strings.Compare(a.OriginNodeID, b.OriginNodeID)
}

func (v Version) Compare(other Version) int {
	return CompareVersion(v, other)
}

// ParseVersion parses the stable revision representation emitted to clients.
// Node IDs may contain dashes, so only the first two separators are structural.
func ParseVersion(value string) (Version, error) {
	parts := strings.SplitN(value, "-", 3)
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("parse version: invalid format")
	}
	physical, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Version{}, fmt.Errorf("parse version physical time: %w", err)
	}
	logical, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return Version{}, fmt.Errorf("parse version logical counter: %w", err)
	}
	version := Version{PhysicalMillis: physical, Logical: logical, OriginNodeID: parts[2]}
	if err := version.Validate(); err != nil {
		return Version{}, fmt.Errorf("parse version: %w", err)
	}
	return version, nil
}
