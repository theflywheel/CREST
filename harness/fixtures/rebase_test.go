package fixtures_test

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/harness/fixtures"
)

func TestLoadKeepsTheFilesDatesByDefault(t *testing.T) {
	w, err := fixtures.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	if !w.Instance.Epoch.Equal(want) {
		t.Fatalf("Load must not move the world: got %s want %s", w.Instance.Epoch, want)
	}
}

// The point of rebasing is that only the anchor moves. Every interval in the
// world — terms signed before the authorization citing them, a qualification
// satisfied before the definition requiring it — has to survive intact, or the
// rebased world is one the services would have refused.
func TestLoadAtMovesTheAnchorAndNothingElse(t *testing.T) {
	base, err := fixtures.Load()
	if err != nil {
		t.Fatal(err)
	}
	to := time.Date(2027, 1, 14, 9, 30, 0, 0, time.UTC)
	moved, err := fixtures.LoadAt(to)
	if err != nil {
		t.Fatal(err)
	}
	if !moved.Instance.Epoch.Equal(to) {
		t.Fatalf("epoch: got %s want %s", moved.Instance.Epoch, to)
	}
	delta := to.Sub(base.Instance.Epoch)

	if len(base.Authorizations) != len(moved.Authorizations) {
		t.Fatalf("rebasing changed the world's shape: %d authorizations became %d",
			len(base.Authorizations), len(moved.Authorizations))
	}
	for i := range base.Authorizations {
		b, m := base.Authorizations[i], moved.Authorizations[i]
		if b.ID != m.ID {
			t.Fatalf("authorization[%d]: rebasing changed an id, %q became %q", i, b.ID, m.ID)
		}
		if got, want := m.ApprovedAt, b.ApprovedAt.Add(delta); !got.Equal(want) {
			t.Errorf("authorization[%d] approvedAt: got %s want %s", i, got, want)
		}
		if got, want := m.Period.Start, b.Period.Start.Add(delta); !got.Equal(want) {
			t.Errorf("authorization[%d] period.start: got %s want %s", i, got, want)
		}
	}
}

// A rebased world still has to validate against the schemas — the load path
// does that itself, so this asserts the failure mode rather than the success:
// a world moved to a date far from the file's must still be a legal world.
func TestLoadAtStillValidates(t *testing.T) {
	if _, err := fixtures.LoadAt(time.Date(2031, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("a rebased world must still be a legal one: %v", err)
	}
}
