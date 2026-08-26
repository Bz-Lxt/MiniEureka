package clock

import "time"

// Source makes wall time injectable without leaking test-only controls into
// production paths.
type Source interface {
	Now() time.Time
}

type SourceFunc func() time.Time

func (f SourceFunc) Now() time.Time { return f() }

type RealSource struct{}

func (RealSource) Now() time.Time { return time.Now() }
