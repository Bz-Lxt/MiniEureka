package gossip

import "testing"

func TestSelectorWithoutReplacement(t *testing.T) {
	t.Parallel()
	selector := NewSelector(7)
	selected := selector.Pick([]string{"a", "b", "c", "d"}, 3)
	if len(selected) != 3 {
		t.Fatalf("selected %d", len(selected))
	}
	seen := map[string]bool{}
	for _, peer := range selected {
		if seen[peer] {
			t.Fatalf("duplicate peer %q", peer)
		}
		seen[peer] = true
	}
}

func TestFaults(t *testing.T) {
	t.Parallel()
	faults := NewFaults(1)
	faults.Set(Fault{PeerNodeID: "n2", DropPercent: 100})
	if !faults.ShouldDrop("n2") || faults.ShouldDrop("n3") {
		t.Fatal("unexpected fault decision")
	}
}
