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
)

func main() {
	ctx := context.Background()
	s := harness.New()
	if err := s.WaitReady(ctx, 90*time.Second); err != nil {
		log.Fatalf("the stack never came up: %v (is it running? make e2e-up)", err)
	}
	w, err := s.Seed(ctx)
	if err != nil {
		log.Fatalf("seed: %v", err)
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
	fmt.Println("web app: http://localhost:59100")
	if os.Getenv("SEED_HOLD") == "true" {
		// One-shot service on a platform that restarts exited containers:
		// seed once, then hold, so the deployment is not re-seeded in a loop.
		fmt.Println("holding (SEED_HOLD=true)")
		select {}
	}
}
