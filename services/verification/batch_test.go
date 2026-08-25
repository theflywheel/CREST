package main

import (
	"log/slog"
	"testing"
)

// The cap is L2 with an L1 floor of existence: a deployment can change the
// number and cannot configure it away. A cap that can be set to "unlimited"
// is a warning wearing a cap's name.
func TestTheBatchCapCanBeMovedButNotRemoved(t *testing.T) {
	log := slog.Default()
	cases := []struct {
		value string
		want  int
	}{
		{"", defaultBatchCap},
		{"25", 25},
		{"0", defaultBatchCap},         // zero is removal, refused
		{"-1", defaultBatchCap},        // so is negative
		{"unlimited", defaultBatchCap}, // and so is nonsense
	}
	for _, c := range cases {
		t.Setenv("CREST_VERIFY_BATCH_CAP", c.value)
		if got := batchCap(log); got != c.want {
			t.Errorf("CREST_VERIFY_BATCH_CAP=%q: got %d, want %d", c.value, got, c.want)
		}
	}
}
