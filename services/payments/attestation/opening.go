package attestation

import (
	"sync"
	"time"
)

// Where a confirmation window's opening instant comes from (#221, ruled
// 2026-09-08).
//
// There is exactly one definition of it, and it is not this process's clock.
// `evidence` stamps the instant the worker could first have seen the record and
// carries it in the `claim.created` handoff; this application opens the window
// at that instant and computes `closesAt` from it. Before #221 the window
// opened at whatever time the payments process read when the HTTP delivery
// landed, which made two different things move a worker's deadline with no
// symptom anywhere:
//
//   - a redelivery. The outbox retries; a delivery held up by an outage arrived
//     hours or days after the claim existed, and the worker was silently given
//     a fresh seven days starting when the network came back. Late-arriving is
//     now what it should be: the window opens at the right time and simply has
//     less of itself left.
//   - skew between the core and payments processes. Nothing compared the two
//     clocks, so a drifting deployment moved every worker's deadline and looked
//     entirely healthy doing it.
//
// The supplied instant is trusted rather than clamped: the peer is
// service-token-authenticated, and a payments process that "corrected" evidence
// with its own clock would be back to reading its own clock. What it does
// instead is record the disagreement — see skewCounter.

// opening is the decision about one window's opening instant.
type opening struct {
	// At is when the window opens, and therefore At+window is when it closes.
	At time.Time

	// Supplied is whether the handoff carried the instant. False means the
	// fallback ran: a payload from a core deployed before #221.
	Supplied bool

	// Delta is this process's arrival clock minus the supplied instant.
	// Positive means payments read a later time than evidence stamped, which
	// is either delivery lag or a clock ahead — the two are not
	// distinguishable from one message, which is why the warning names both.
	// Zero when nothing was supplied to compare against.
	Delta time.Duration
}

// decideOpening is the whole rule, kept pure so it can be tested as a truth
// table rather than through a stack.
//
// The fallback is deliberately silent about correctness and loud in the log:
// with no supplied instant, the arrival clock is the only instant this process
// has, and using it reproduces the pre-#221 behaviour for exactly as long as a
// core without the field is still deployed. See DEPLOYMENT.md — one deploy.
func decideOpening(supplied, arrived time.Time) opening {
	if supplied.IsZero() {
		return opening{At: arrived}
	}
	return opening{At: supplied, Supplied: true, Delta: arrived.Sub(supplied)}
}

// closesFrom is the only place a window's closing instant is computed, and
// there are two callers because a window's opening instant is decided twice.
//
// The first is the handoff, above. The second is an acknowledgement — the
// worker following their review link, or a supervisor acknowledging for a
// worker who has no phone — which restarts the seven days from the moment the
// record was actually put in front of somebody. #221's third clause is that
// both follow the same rule, and the rule is "the window runs from the instant
// the worker could first have seen the record". For an acknowledgement that
// instant is this process's own clock and legitimately so: the acknowledgement
// is a request payments is handling, not a fact another process is reporting,
// so there is no second clock to prefer and nothing to skew against.
func closesFrom(opensAt time.Time, window time.Duration) time.Time {
	return opensAt.Add(window)
}

// exceeds reports whether the disagreement between the two clocks is worth
// alerting on. Absolute, because payments behind evidence is the same illness
// as payments ahead of it.
func (o opening) exceeds(threshold time.Duration) bool {
	if !o.Supplied || threshold <= 0 {
		return false
	}
	d := o.Delta
	if d < 0 {
		d = -d
	}
	return d > threshold
}

// skewCounter is the readable half of the detector: a log line nobody greps is
// not a detector, so the same events are counted and published on
// /internal/metrics (service.Options.Metrics).
//
// Small and in memory on purpose. It answers "is this deployment's core
// disagreeing with its payments about what time it is", which is a question
// about the process that is running now; persisting it would make it a record,
// and a record of an operational symptom is not a worker's record.
type skewCounter struct {
	mu        sync.Mutex
	events    float64       // handoffs whose delta exceeded the threshold
	fallbacks float64       // handoffs that carried no instant at all
	worst     time.Duration // largest absolute delta seen, signed away
}

func (c *skewCounter) observe(o opening, threshold time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !o.Supplied {
		c.fallbacks++
		return
	}
	d := o.Delta
	if d < 0 {
		d = -d
	}
	if d > c.worst {
		c.worst = d
	}
	if o.exceeds(threshold) {
		c.events++
	}
}

// windowSkew is the process's one counter. Package-level because the member is
// composed exactly once into the payments process and the Metrics hook is read
// from Service() while the handlers are built later, in windowRoutes; a field
// would have to be reachable from both and is the same single instance with
// more wiring.
var windowSkew skewCounter

func (c *skewCounter) snapshot() (events, fallbacks float64, worst time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events, c.fallbacks, c.worst
}
