package clock

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"minieureka/internal/model"
)

var ErrRemoteClockTooFarAhead = errors.New("remote clock exceeds allowed future skew")

const DefaultMaxFutureSkew = 30 * time.Second

type HLC struct {
	mu            sync.Mutex
	nodeID        string
	source        Source
	maxFutureSkew time.Duration
	last          model.Version
}

type Option func(*HLC)

func WithSource(source Source) Option {
	return func(c *HLC) {
		if source != nil {
			c.source = source
		}
	}
}

func WithMaxFutureSkew(skew time.Duration) Option {
	return func(c *HLC) { c.maxFutureSkew = skew }
}

func New(nodeID string, options ...Option) (*HLC, error) {
	// Reuse Version validation so node IDs obey the same wire constraints.
	probe := model.Version{PhysicalMillis: 1, OriginNodeID: nodeID}
	if err := probe.Validate(); err != nil {
		return nil, fmt.Errorf("create HLC: %w", err)
	}
	c := &HLC{
		nodeID:        nodeID,
		source:        RealSource{},
		maxFutureSkew: DefaultMaxFutureSkew,
	}
	for _, option := range options {
		option(c)
	}
	if c.maxFutureSkew < 0 {
		return nil, fmt.Errorf("create HLC: max future skew must not be negative")
	}
	return c, nil
}

// Now returns a version greater than every version previously generated or
// observed by this HLC, even when the wall clock moves backwards.
func (c *HLC) Now() model.Version {
	c.mu.Lock()
	defer c.mu.Unlock()

	physical := c.source.Now().UnixMilli()
	if physical > c.last.PhysicalMillis {
		c.last = model.Version{PhysicalMillis: physical, OriginNodeID: c.nodeID}
		return c.last
	}

	c.last.PhysicalMillis = max(c.last.PhysicalMillis, physical)
	c.last.Logical = nextLogical(&c.last.PhysicalMillis, c.last.Logical)
	c.last.OriginNodeID = c.nodeID
	return c.last
}

// Observe incorporates a remote HLC value and returns a new local value. A
// bounded future-skew check prevents one bad peer from pinning the cluster's
// logical time far into the future.
func (c *HLC) Observe(remote model.Version) (model.Version, error) {
	if err := remote.Validate(); err != nil {
		return model.Version{}, fmt.Errorf("observe remote version: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.source.Now().UnixMilli()
	if remote.PhysicalMillis > now+c.maxFutureSkew.Milliseconds() {
		return model.Version{}, ErrRemoteClockTooFarAhead
	}

	localPhysical := c.last.PhysicalMillis
	physical := max(now, max(localPhysical, remote.PhysicalMillis))
	var logical uint64
	switch {
	case physical == localPhysical && physical == remote.PhysicalMillis:
		logical = max(c.last.Logical, remote.Logical)
		logical = nextLogical(&physical, logical)
	case physical == localPhysical:
		logical = nextLogical(&physical, c.last.Logical)
	case physical == remote.PhysicalMillis:
		logical = nextLogical(&physical, remote.Logical)
	default:
		logical = 0
	}

	c.last = model.Version{PhysicalMillis: physical, Logical: logical, OriginNodeID: c.nodeID}
	return c.last, nil
}

func (c *HLC) Last() model.Version {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *HLC) NodeID() string { return c.nodeID }

func nextLogical(physical *int64, current uint64) uint64 {
	if current != math.MaxUint64 {
		return current + 1
	}
	if *physical != math.MaxInt64 {
		(*physical)++
		return 0
	}
	// This requires a physically impossible amount of logical-clock traffic at
	// the final int64 millisecond. Retaining MaxUint64 preserves monotonicity.
	return math.MaxUint64
}
