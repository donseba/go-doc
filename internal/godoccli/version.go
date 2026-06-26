package godoccli

import (
	"runtime/debug"
	"strings"
)

var Version = detectVersion()

func detectVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" || version == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(version, "v")
}
