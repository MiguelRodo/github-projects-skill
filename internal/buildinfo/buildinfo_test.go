package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestWithBuildInfoFallsBackToModuleAndVCS(t *testing.T) {
	embedded := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.4.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	got := withBuildInfo(Info{Version: "dev", Commit: "unknown", Date: "unknown"}, embedded)
	want := Info{Version: "v1.4.0", Commit: "abc123-modified", Date: "2026-01-02T03:04:05Z"}
	if got != want {
		t.Fatalf("info = %+v, want %+v", got, want)
	}
}

func TestWithBuildInfoKeepsLinkerValues(t *testing.T) {
	embedded := &debug.BuildInfo{
		Main:     debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "other"}},
	}
	linked := Info{Version: "1.2.3", Commit: "deadbeef", Date: "2026-02-03T00:00:00Z"}
	if got := withBuildInfo(linked, embedded); got != linked {
		t.Fatalf("info = %+v, want %+v", got, linked)
	}
}

func TestWithBuildInfoIgnoresDevelModule(t *testing.T) {
	got := withBuildInfo(Info{Version: "dev", Commit: "unknown", Date: "unknown"}, &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}})
	if got.Version != "dev" || got.Commit != "unknown" {
		t.Fatalf("info = %+v", got)
	}
}
