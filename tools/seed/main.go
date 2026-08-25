// Command seed loads the fixture world into a running stack — the same seed
// the e2e harness runs before every scenario, callable on its own so the web
// app (apps/web) has somebody to be without running the test suite first.
package main

import (
	"context"
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
	fmt.Println("web app: http://localhost:59100")
	if os.Getenv("SEED_HOLD") == "true" {
		// One-shot service on a platform that restarts exited containers:
		// seed once, then hold, so the deployment is not re-seeded in a loop.
		fmt.Println("holding (SEED_HOLD=true)")
		select {}
	}
}
