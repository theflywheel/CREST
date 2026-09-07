package httpx

import (
	"os"
	"runtime/debug"
)

// BuildRevision is supplied when building a deployable image.
var BuildRevision = "development"

// transparencyMode reports which registry/transparency substrate this
// process is configured against: the real DeDi node, or the Postgres
// fallback (#20). Both DEDI_URL and DEDI_PUBLISHER_KEY are required for the
// node — see infra/compose/docker-compose.yml — so either being empty means
// the fallback.
//
// Reported on /healthz so the harness preflight (#79) can catch a stack
// pointed at the deployed DeDi node when a scenario run expects the local
// fallback, or the reverse, before any scenario assertion runs rather than
// after a mystifying 500.
func transparencyMode() string {
	if os.Getenv("DEDI_URL") != "" && os.Getenv("DEDI_PUBLISHER_KEY") != "" {
		return "dedi"
	}
	return "postgres"
}

func buildRevision() string {
	if BuildRevision != "development" {
		return BuildRevision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return BuildRevision
	}
	revision, dirty := "", false
	for _, v := range info.Settings {
		if v.Key == "vcs.revision" {
			revision = v.Value
		}
		if v.Key == "vcs.modified" {
			dirty = v.Value == "true"
		}
	}
	if revision == "" {
		return BuildRevision
	}
	if dirty {
		revision += "+dirty"
	}
	return revision
}
