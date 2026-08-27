// Command seed loads the fixture world into a running stack — the same seed
// the e2e harness runs before every scenario, callable on its own so the web
// app (apps/web) has somebody to be without running the test suite first.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/pkg/clock"
)

func main() {
	ctx := context.Background()
	s := harness.New()
	if err := s.WaitReady(ctx, 90*time.Second); err != nil {
		log.Fatalf("the stack never came up: %v (is it running? make e2e-up)", err)
	}
	// Where the world's dates sit. A demo deployment wants them around today
	// and wants the clock still running afterwards; the test stacks want the
	// file's own fixed dates and a clock only they move. SEED_LIVE_CLOCK
	// chooses, and defaults to whatever SEED_STORY is: telling the story is
	// the demo case.
	live := os.Getenv("SEED_LIVE_CLOCK") == "true" ||
		(os.Getenv("SEED_LIVE_CLOCK") == "" && os.Getenv("SEED_STORY") == "true")
	var epoch time.Time
	if live {
		// Far enough back that the story's eight days land on today.
		epoch = clock.System{}.Now().Add(-harness.StoryClockAdvance)
	}
	w, err := s.SeedAt(ctx, epoch)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	if live {
		fmt.Printf("seeding from %s so the story ends at about now\n", epoch.Format(time.RFC3339))
	}
	fmt.Printf("seeded %q: %d parties, %d authorizations, %d definitions\n",
		w.Instance.Name, len(w.Parties), len(w.Authorizations), len(w.Definitions))

	if os.Getenv("SEED_STORY") == "true" {
		// The story is a demo layer over the seed, and it is opt-in: the e2e
		// scenarios assume a bare fixture world and must never find a
		// pre-disputed claim in it.
		switch err := s.SeedStory(ctx, w); {
		case errors.Is(err, harness.ErrStoryAlreadySeeded):
			fmt.Println("story already seeded (the riverside-dhis2 source is registered); left as is")
		case err != nil:
			log.Fatalf("story: %v", err)
		default:
			fmt.Println("story seeded: 3 consents, 2 sources graded, 8 claims (2 self-confirmed, " +
				"1 auto, 1 assisted, 1 disputed, 3 open), 1 held payment, 1 unclear row, " +
				"2 verifications, 1 duplicate hold, 1 open recovery, 1 overdue authorization")
		}
	}
	if live {
		// The clock goes back on real time, ticking. Left where the story
		// stopped, nothing in the stack could ever become due again: no window
		// would reach T=7, and the auto-confirm exit — the one no person
		// triggers — would never fire for anyone.
		if err := s.LiveClock(ctx); err != nil {
			log.Fatalf("hand the clock back to real time: %v", err)
		}
		fmt.Println("clock is live: the stack now runs on real time and windows come due on their own")
	}

	fmt.Println("web app: http://localhost:59100")
}
