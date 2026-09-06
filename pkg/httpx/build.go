package httpx

import "runtime/debug"

// BuildRevision is supplied when building a deployable image.
var BuildRevision = "development"

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
