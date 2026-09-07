// Command e2e-fingerprint records and checks what stack `make e2e-up` last
// brought up, and prints the run header `make test-e2e`/`make e2e-run` show
// before anything else runs.
//
// #79: an `e2e-run` against a stack left up from a previous, differently
// configured invocation never tears volumes down, so state leaks across runs
// undetected. `write` (called by `e2e-up`) records a fingerprint of the
// compose config plus the environment variables that shape it, and a start
// timestamp. `check` (called by `e2e-run`, before the harness binary starts)
// recomputes the same fingerprint from the current environment and fails —
// loudly, once, before any test runs — the moment it disagrees. `header`
// prints the run header both targets show.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fingerprintEnv is every environment variable that changes what the stack
// actually does — the ones docker-compose.yml reads to decide the
// transparency substrate, the window and sweep cadence, and whether the
// payment subscriber runs at all. A variable that does not change behaviour
// (a host port override, say) does not belong here: including it would make
// the fingerprint flag developers apart who are running the identical stack
// on different machines.
var fingerprintEnv = []string{
	"DEDI_URL",
	"DEDI_PUBLISHER_KEY",
	"DEDI_NAMESPACE",
	"CONFIRMATION_WINDOW",
	"SWEEP_EVERY",
	"PAYMENT_SUBSCRIBER_ENABLED",
	"COMPOSE_FILE",
}

const fingerprintPath = ".e2e/stack.fingerprint"

type fingerprint struct {
	Hash      string            `json:"hash"`
	StartedAt time.Time         `json:"startedAt"`
	Env       map[string]string `json:"env"`
	Compose   string            `json:"composeFile"`
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: e2e-fingerprint <write|check|header>")
	}
	root, err := repoRoot()
	if err != nil {
		fatal("finding repo root: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		fatal("chdir to repo root: %v", err)
	}

	switch os.Args[1] {
	case "mark-start":
		cmdMarkStart()
	case "write":
		cmdWrite()
	case "check":
		cmdCheck()
	case "header":
		cmdHeader()
	default:
		fatal("unknown subcommand %q", os.Args[1])
	}
}

const startMarkerPath = ".e2e/run-started"

// cmdMarkStart records when this invocation began, before docker compose
// does anything. The header's "db volume predates run" line needs this: a
// `--build --wait` can easily take longer than any fixed threshold, so
// comparing a volume's CreatedAt against "now" at header time — after the
// build and the wait — would call a volume this same invocation just created
// "pre-existing" purely because bringing the stack up took a while.
func cmdMarkStart() {
	if err := os.MkdirAll(filepath.Dir(startMarkerPath), 0o755); err != nil {
		fatal("creating .e2e/: %v", err)
	}
	if err := os.WriteFile(startMarkerPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644); err != nil {
		fatal("writing %s: %v", startMarkerPath, err)
	}
}

func runStart() (time.Time, bool) {
	raw, err := os.ReadFile(startMarkerPath)
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(raw)))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func composeFile() string {
	if v := os.Getenv("COMPOSE_FILE"); v != "" {
		return v
	}
	return "infra/compose/docker-compose.yml"
}

func currentEnv() map[string]string {
	out := make(map[string]string, len(fingerprintEnv))
	for _, k := range fingerprintEnv {
		out[k] = os.Getenv(k)
	}
	return out
}

func computeHash(env map[string]string, composeContents []byte) string {
	h := sha256.New()
	h.Write(composeContents)
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, env[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func cmdWrite() {
	path := composeFile()
	contents, err := os.ReadFile(path)
	if err != nil {
		fatal("reading %s: %v", path, err)
	}
	env := currentEnv()
	fp := fingerprint{
		Hash:      computeHash(env, contents),
		StartedAt: time.Now().UTC(),
		Env:       env,
		Compose:   path,
	}
	if err := os.MkdirAll(filepath.Dir(fingerprintPath), 0o755); err != nil {
		fatal("creating .e2e/: %v", err)
	}
	out, err := json.MarshalIndent(fp, "", "  ")
	if err != nil {
		fatal("encoding fingerprint: %v", err)
	}
	if err := os.WriteFile(fingerprintPath, out, 0o644); err != nil {
		fatal("writing %s: %v", fingerprintPath, err)
	}
	fmt.Printf("wrote %s (hash %s)\n", fingerprintPath, fp.Hash[:12])
}

func cmdCheck() {
	raw, err := os.ReadFile(fingerprintPath)
	if err != nil {
		// No fingerprint at all is its own kind of stale environment: a stack
		// nobody brought up through `make e2e-up` (or one whose fingerprint was
		// cleared by `make e2e-reset`) is exactly the situation the harness must
		// not silently assume is fine.
		staleFail([]string{
			fmt.Sprintf("no stack fingerprint found — expected %s", fingerprintPath),
		}, "was `make e2e-up` run before `make e2e-run`? (or the stack was reset with `make e2e-reset` and not brought back up)")
	}
	var fp fingerprint
	if err := json.Unmarshal(raw, &fp); err != nil {
		fatal("parsing %s: %v", fingerprintPath, err)
	}

	path := composeFile()
	contents, err := os.ReadFile(path)
	if err != nil {
		fatal("reading %s: %v", path, err)
	}
	env := currentEnv()
	current := computeHash(env, contents)

	if current == fp.Hash {
		return
	}

	var mismatches []string
	for _, k := range fingerprintEnv {
		if fp.Env[k] != env[k] {
			mismatches = append(mismatches, fmt.Sprintf("%s: stack was started with %q, this run expects %q", k, fp.Env[k], env[k]))
		}
	}
	if len(mismatches) == 0 {
		// The env variables tracked matched, so the compose file itself changed
		// under a stack that is still running the old one.
		mismatches = append(mismatches, fmt.Sprintf("compose config changed since the stack was started (fingerprint hash %s, current %s)", fp.Hash[:12], current[:12]))
	}
	staleFail(mismatches, "`make e2e-reset` tears the stack down with its volumes, then `make e2e-up` brings up a stack that matches this run.")
}

func staleFail(mismatches []string, hint string) {
	var b strings.Builder
	b.WriteString("stale environment: the running stack does not match what this harness run expects\n")
	for _, m := range mismatches {
		b.WriteString("  - ")
		b.WriteString(m)
		b.WriteString("\n")
	}
	b.WriteString(hint)
	fmt.Fprintln(os.Stderr, b.String())
	os.Exit(1)
}

// cmdHeader prints the block `make test-e2e`/`make e2e-run` show before any
// scenario runs: the transparency mode, clock mode, service versions, the
// compose project name, and whether the database volume predates this run.
func cmdHeader() {
	env := currentEnv()
	transparency := "postgres"
	if env["DEDI_URL"] != "" && env["DEDI_PUBLISHER_KEY"] != "" {
		transparency = "dedi"
	}
	window := env["CONFIRMATION_WINDOW"]
	if window == "" {
		window = "168h (default)"
	}
	sweep := env["SWEEP_EVERY"]
	clockMode := "driveable (services take an Offset clock; the harness moves it — see docs/TESTING.md)"

	rev := gitRevision()
	projectName := composeProjectName()
	volumeAge := postgresVolumeAge(projectName)

	fmt.Println("── e2e run header ──────────────────────────────────────────")
	fmt.Printf("transparency substrate : %s\n", transparency)
	fmt.Printf("clock mode             : %s\n", clockMode)
	fmt.Printf("confirmation window    : %s\n", window)
	if sweep != "" {
		fmt.Printf("sweep interval          : %s\n", sweep)
	}
	fmt.Printf("service revision       : %s\n", rev)
	fmt.Printf("compose project        : %s\n", projectName)
	fmt.Printf("db volume predates run : %s\n", volumeAge)
	fmt.Println("────────────────────────────────────────────────────────────")
}

func gitRevision() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown (not a git checkout, or git unavailable)"
	}
	rev := strings.TrimSpace(string(out))
	dirty, err := exec.Command("git", "status", "--porcelain").Output()
	if err == nil && len(strings.TrimSpace(string(dirty))) > 0 {
		rev += "+dirty"
	}
	return rev
}

func composeProjectName() string {
	// docker-compose.yml declares `name: crest` at the top level; an explicit
	// COMPOSE_PROJECT_NAME (as harness/stack.go's compose() and the Makefile's
	// $(COMPOSE) both honour) overrides it, e.g. to run an isolated e2e stack
	// alongside another one on the same Docker daemon.
	if v := os.Getenv("COMPOSE_PROJECT_NAME"); v != "" {
		return v
	}
	return "crest"
}

// postgresVolumeAge reports whether the compose project's Postgres volume
// existed before this invocation, by comparing the volume's CreatedAt to
// this process's own start time. A volume older than "now" by more than a
// few seconds predates this run, which is exactly the state test-e2e's
// leading `down -v` is supposed to prevent and e2e-run has no such guard for.
func postgresVolumeAge(project string) string {
	name := project + "_pgdata"
	out, err := exec.Command("docker", "volume", "inspect", name, "--format", "{{.CreatedAt}}").Output()
	if err != nil {
		return "no volume found (fresh — or the stack is not up)"
	}
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(string(out)))
	if err != nil {
		return "unknown (could not parse volume creation time)"
	}
	start, haveMarker := runStart()
	if !haveMarker {
		// No marker (header run without mark-start, e.g. by hand): fall back to
		// a fixed threshold against "now" — informational only.
		age := time.Since(created)
		if age < 30*time.Second {
			return fmt.Sprintf("no — created %s ago, likely by this invocation", age.Round(time.Second))
		}
		return fmt.Sprintf("yes — created %s ago (predates this run)", age.Round(time.Second))
	}
	// A few seconds of slack: the marker is written before `docker compose up`
	// even starts talking to the daemon, so a volume created a moment after it
	// is still this invocation's own.
	if created.After(start.Add(-2 * time.Second)) {
		return fmt.Sprintf("no — created %s, after this run started", created.Format(time.RFC3339))
	}
	return fmt.Sprintf("yes — created %s, before this run started at %s (predates this run)",
		created.Format(time.RFC3339), start.Format(time.RFC3339))
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", wd)
		}
		dir = parent
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
