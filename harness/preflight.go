package harness

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
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
// from /healthz and /internal/clock.
type ServiceStatus struct {
	Transparency string // "dedi" | "postgres", from GET /healthz
	Revision     string // build/version fingerprint, from GET /healthz
	ClockTicking bool   // true when the service's clock is a driveable Offset (GET /internal/clock reports "ticking")
}

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
//   - every service's clock must be a driveable Offset clock ("ticking"),
//     because the harness moves time rather than sleeping (docs/TESTING.md)
//     and a service that cannot be driven fails every window-dependent
//     scenario for a reason that has nothing to do with the scenario;
//   - and every service's build revision must agree with every other
//     service's, because one image rebuilt and another left stale from a
//     previous run is the same "environment vs. defect" confusion one layer
//     down.
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
		if !st.ClockTicking {
			out = append(out, Mismatch{
				Service: name, Field: "clock mode",
				Expected: "driveable (ticking)", Actual: "not driveable",
			})
		}
		if st.Revision != "" {
			revisions[st.Revision] = true
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
		Transparency string `json:"transparency"`
		Revision     string `json:"revision"`
	}
	if err := svc.Get(ctx, "/healthz", &health); err != nil {
		return ServiceStatus{}, err
	}
	var clk struct {
		Ticking bool `json:"ticking"`
	}
	if err := svc.Get(ctx, "/internal/clock", &clk); err != nil {
		return ServiceStatus{}, err
	}
	return ServiceStatus{
		Transparency: health.Transparency,
		Revision:     health.Revision,
		ClockTicking: clk.Ticking,
	}, nil
}
