package hatch

import "runtime/debug"

// releaseVersion is set by the release packager using -ldflags -X.
var releaseVersion string

func version() string {
	info, _ := debug.ReadBuildInfo()
	return buildVersion(releaseVersion, info)
}

func buildVersion(release string, info *debug.BuildInfo) string {
	if release != "" {
		return release
	}
	if info != nil {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return "dev"
			}
		}
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
