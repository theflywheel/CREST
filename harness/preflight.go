package harness

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Preflight distinguishes a stale environment from a real defect (#79).
//
// #79 was filed after `make test-e2e` failed every scenario at
// `POST /v1/parties -> 500` with no code change involved: the stack had been
// started by hand against the deployed DeDi node, and a later run expected
// the Postgres fallback. Nothing said so — the mismatch surfaced twenty
// identical scenario failures downstream, which reads exactly like a broken
// build. The fix is to ask the running stack what it thinks it is, before any
// scenario assertion runs, and fail once with a message that names the
// mismatch.

// ServiceStatus is what one running service reports about itself, gathered
// from /healthz.
type ServiceStatus struct {
	Transparency string // "dedi" | "postgres", from GET /healthz
	Revision     string // build/version fingerprint, from GET /healthz

	// Now is what this service thinks the time is, from /healthz's `time`.
	//
	// It is here because the confirmation window's opening instant is stamped
	// by one process and honoured by another (#221). Two processes that
	// disagree about the time move a worker's deadline, silently, in a way
	// that reads as a scenario asserting the wrong number rather than as a
	// stack that is wrong. It matters more now than it did, not less: with no
	// clock to align, nothing puts these two processes back in step — the
	// host's timekeeping is the only thing holding them together.
	//
	// Zero means the service reported no time at all, and the skew rule then
	// says nothing rather than guessing.
	Now time.Time
}

// ClockSkewThreshold is how far two processes' clocks may differ before the
// stack is stale-environment-class.
//
// The same five minutes as the payments application's CLOCK_SKEW_ALERT
// production default, deliberately: the harness should fail on the
// disagreement a deployment would warn about, not on a tighter or looser one
// of its own. It is generous on purpose — the statuses are gathered one
// service at a time over HTTP, so a small difference is the gathering, not the
// stack. Note that a harness stack sets CLOCK_SKEW_ALERT to about a second so
// the detector itself can be exercised; this threshold is the harness's own
// judgement about a broken stack and is deliberately not that value.
const ClockSkewThreshold = 5 * time.Minute

// ExpectedConfig is what this harness invocation expects of the stack it is
// about to run against, derived from its own environment — the same
// DEDI_URL/DEDI_PUBLISHER_KEY variables a developer or `make e2e-up` sets.
type ExpectedConfig struct {
	Transparency string // "dedi" | "postgres"
}

// ExpectedConfigFromEnv reads the expectation the harness process itself was
// invoked with, the same way infra/compose/docker-compose.yml derives it for
// the services it starts.
func ExpectedConfigFromEnv() ExpectedConfig {
	if os.Getenv("DEDI_URL") != "" && os.Getenv("DEDI_PUBLISHER_KEY") != "" {
		return ExpectedConfig{Transparency: "dedi"}
	}
	return ExpectedConfig{Transparency: "postgres"}
}

// Mismatch is one disagreement between what the harness expects and what a
// service reports.
type Mismatch struct {
	Service  string
	Field    string
	Expected string
	Actual   string
}

func (m Mismatch) String() string {
	return fmt.Sprintf("%s: %s expected %q, got %q", m.Service, m.Field, m.Expected, m.Actual)
}

// ComparePreflight is the pure comparison at the heart of the preflight
// check: expected configuration against what each service actually reports.
// No network, no Docker — this is what the unit test in preflight_test.go
// exercises directly.
//
// Two rules, both drawn from #79's own account of the incident:
//   - every service's transparency substrate must agree with what this
//     harness invocation expects (a stack half-pointed at the deployed DeDi
//     node is the exact failure mode #79 names);
//   - every service's build revision must agree with every other
//     service's, because one image rebuilt and another left stale from a
//     previous run is the same "environment vs. defect" confusion one layer
//     down;
//   - and every service must think it is roughly the same time as every other
//     (#221). The confirmation window's opening instant is stamped by core and
//     honoured by payments, so two processes that disagree about the time move
//     a worker's deadline — silently, and in a way that reads as a scenario
//     asserting the wrong number rather than as a stack that is wrong.
//
// Deterministic order: mismatches are returned sorted by service then field,
// so a repeated run reports the same thing in the same order.
func ComparePreflight(expected ExpectedConfig, actual map[string]ServiceStatus) []Mismatch {
	var out []Mismatch

	names := make([]string, 0, len(actual))
	for name := range actual {
		names = append(names, name)
	}
	sort.Strings(names)

	revisions := map[string]bool{}
	for _, name := range names {
		st := actual[name]
		if expected.Transparency != "" && st.Transparency != "" && st.Transparency != expected.Transparency {
			out = append(out, Mismatch{
				Service: name, Field: "transparency substrate",
				Expected: expected.Transparency, Actual: st.Transparency,
			})
		}
		if st.Revision != "" {
			revisions[st.Revision] = true
		}
	}
	// Two processes that disagree about the time (#221). Simple and honest: the
	// earliest reported time against the latest, one mismatch naming both, and
	// nothing said at all unless at least two services reported a time.
	//
	// Every process is on real time now, aligned only by whatever keeps the
	// host's clock, so a difference here is drift and nothing else. The stack
	// then cannot be trusted about when a window closes, and that is what the
	// failure says.
	var earliest, latest string
	for _, name := range names {
		if actual[name].Now.IsZero() {
			continue
		}
		if earliest == "" || actual[name].Now.Before(actual[earliest].Now) {
			earliest = name
		}
		if latest == "" || actual[name].Now.After(actual[latest].Now) {
			latest = name
		}
	}
	if earliest != "" && latest != earliest {
		if skew := actual[latest].Now.Sub(actual[earliest].Now); skew > ClockSkewThreshold {
			out = append(out, Mismatch{
				Service: "stack", Field: "clock skew",
				Expected: fmt.Sprintf("every process within %s of every other", ClockSkewThreshold),
				Actual: fmt.Sprintf("%s is %s ahead of %s (%s vs %s); a confirmation window opened by one "+
					"and closed by the other would move a worker's deadline",
					latest, skew, earliest,
					actual[latest].Now.Format(time.RFC3339), actual[earliest].Now.Format(time.RFC3339)),
			})
		}
	}

	// A build-revision mismatch is only meaningful once there is more than one
	// distinct revision reported: nothing to compare with a single service, or
	// with services that report no revision at all (BuildRevision unset).
	if len(revisions) > 1 {
		rev := make([]string, 0, len(revisions))
		for r := range revisions {
			rev = append(rev, r)
		}
		sort.Strings(rev)
		for _, name := range names {
			out = append(out, Mismatch{
				Service: name, Field: "build revision",
				Expected: "the same revision as every other service",
				Actual:   fmt.Sprintf("%s (stack reports: %s)", actual[name].Revision, strings.Join(rev, ", ")),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// StaleEnvironmentError is the single, explicit failure the preflight raises
// on any mismatch. Its message always contains the phrase "stale
// environment" — that is the string a person or CI log scan should be able
// to grep for — and names expected vs. actual for every mismatch found.
type StaleEnvironmentError struct {
	Mismatches []Mismatch
}

func (e *StaleEnvironmentError) Error() string {
	var b strings.Builder
	b.WriteString("stale environment: the running stack does not match what this harness run expects\n")
	for _, m := range e.Mismatches {
		b.WriteString("  - ")
		b.WriteString(m.String())
		b.WriteString("\n")
	}
	b.WriteString("this is not necessarily a code defect: `make e2e-reset` tears the stack " +
		"down with its volumes, then `make e2e-up` brings up a stack that matches this " +
		"invocation's environment.")
	return b.String()
}

// preflightOnce runs the check exactly once per test binary. Every scenario
// package's setup() calls Preflight, and there are ~20 scenario tests in one
// package/binary (`go test ./harness/...` compiles one binary per package) —
// repeating an HTTP round trip to every service ahead of every single test
// would be wasteful and would print the same header-shaped failure twenty
// times over for one cause.
var preflightOnce sync.Once
var preflightErr error

// Preflight checks the running stack against what this harness invocation
// expects, exactly once per process, before any scenario assertion runs. It
// fails loudly and singularly on the first mismatch found — see
// StaleEnvironmentError.
func (s *Stack) Preflight(ctx context.Context) error {
	preflightOnce.Do(func() {
		preflightErr = s.runPreflight(ctx)
	})
	return preflightErr
}

func (s *Stack) runPreflight(ctx context.Context) error {
	expected := ExpectedConfigFromEnv()
	actual := map[string]ServiceStatus{}
	for _, svc := range s.Services() {
		st, err := gatherServiceStatus(ctx, svc)
		if err != nil {
			// A service that cannot even answer /healthz is a stack that is not
			// up, which WaitReady already reports on its own terms; the
			// preflight only speaks to stacks that answered but disagree.
			continue
		}
		actual[svc.Name] = st
	}
	if len(actual) == 0 {
		return nil
	}
	if mismatches := ComparePreflight(expected, actual); len(mismatches) > 0 {
		return &StaleEnvironmentError{Mismatches: mismatches}
	}
	return nil
}

func gatherServiceStatus(ctx context.Context, svc *Service) (ServiceStatus, error) {
	var health struct {
		Transparency string    `json:"transparency"`
		Revision     string    `json:"revision"`
		Time         time.Time `json:"time"`
	}
	if err := svc.Get(ctx, "/healthz", &health); err != nil {
		return ServiceStatus{}, err
	}
	// /healthz's `time` is the process's own real UTC clock, and it is the
	// only place the harness asks: there is no /internal/clock any more.
	return ServiceStatus{
		Transparency: health.Transparency,
		Revision:     health.Revision,
		Now:          health.Time,
	}, nil
}
