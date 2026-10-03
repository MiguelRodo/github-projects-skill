package buildinfo

import "runtime/debug"

// These values are replaced by GoReleaser. Keeping useful development values
// makes local builds honest without requiring linker flags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// Info describes the binary that is currently running.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Current returns build information embedded in the binary. Release builds
// carry linker-supplied values. Other builds, such as `go install
// .../cmd/projects@vX.Y.Z` or a local `go build`, fall back to the module
// version and VCS stamps that the Go toolchain records.
func Current() Info {
	info := Info{Version: version, Commit: commit, Date: date}
	embedded, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	return withBuildInfo(info, embedded)
}

func withBuildInfo(info Info, embedded *debug.BuildInfo) Info {
	if embedded == nil {
		return info
	}
	if info.Version == "dev" {
		if moduleVersion := embedded.Main.Version; moduleVersion != "" && moduleVersion != "(devel)" {
			info.Version = moduleVersion
		}
	}
	settings := make(map[string]string, len(embedded.Settings))
	for _, setting := range embedded.Settings {
		settings[setting.Key] = setting.Value
	}
	if info.Commit == "unknown" && settings["vcs.revision"] != "" {
		info.Commit = settings["vcs.revision"]
		if settings["vcs.modified"] == "true" {
			info.Commit += "-modified"
		}
	}
	if info.Date == "unknown" && settings["vcs.time"] != "" {
		info.Date = settings["vcs.time"]
	}
	return info
}
