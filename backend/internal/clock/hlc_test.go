package clock

import (
	"errors"
	"sync"
	"testing"
	"time"

	"minieureka/internal/model"
)

type fakeSource struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeSource) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeSource) set(now time.Time) {
	f.mu.Lock()
	f.now = now
	f.mu.Unlock()
}

func TestHLCNowSurvivesWallClockRollback(t *testing.T) {
	source := &fakeSource{now: time.UnixMilli(10_000)}
	hlc, err := New("node-a", WithSource(source))
	if err != nil {
		t.Fatal(err)
	}
	first := hlc.Now()
	source.set(time.UnixMilli(9_000))
	second := hlc.Now()
	if model.CompareVersion(second, first) <= 0 {
		t.Fatalf("rollback moved HLC backwards: first=%v second=%v", first, second)
	}
	if second.PhysicalMillis != first.PhysicalMillis || second.Logical != first.Logical+1 {
		t.Fatalf("unexpected rollback value: %+v", second)
	}
}

func TestHLCObserveRules(t *testing.T) {
	source := &fakeSource{now: time.UnixMilli(10_000)}
	hlc, _ := New("node-a", WithSource(source), WithMaxFutureSkew(time.Second))
	remote := model.Version{PhysicalMillis: 10_500, Logical: 8, OriginNodeID: "node-b"}
	observed, err := hlc.Observe(remote)
	if err != nil {
		t.Fatal(err)
	}
	if observed.PhysicalMillis != remote.PhysicalMillis || observed.Logical != 9 || observed.OriginNodeID != "node-a" {
		t.Fatalf("unexpected observed version: %+v", observed)
	}
	_, err = hlc.Observe(model.Version{PhysicalMillis: 11_001, OriginNodeID: "node-b"})
	if !errors.Is(err, ErrRemoteClockTooFarAhead) {
		t.Fatalf("future version error = %v", err)
	}
}

func TestHLCConcurrentNowIsUniqueAndOrdered(t *testing.T) {
	source := &fakeSource{now: time.UnixMilli(10_000)}
	hlc, _ := New("node-a", WithSource(source))
	const workers = 16
	const each = 200
	versions := make(chan model.Version, workers*each)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				versions <- hlc.Now()
			}
		}()
	}
	wg.Wait()
	close(versions)
	seen := make(map[model.Version]struct{}, workers*each)
	for version := range versions {
		if _, exists := seen[version]; exists {
			t.Fatalf("duplicate HLC version: %+v", version)
		}
		seen[version] = struct{}{}
	}
	if len(seen) != workers*each {
		t.Fatalf("got %d unique versions", len(seen))
	}
}
