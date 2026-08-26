package demo

import "testing"

func TestNewDefaults(t *testing.T) {
	t.Parallel()
	manager := New(nil, Options{InstanceCount: -1})
	if manager.opts.InstanceCount != 0 || manager.opts.HeartbeatEvery <= 0 {
		t.Fatalf("defaults = %#v", manager.opts)
	}
}
