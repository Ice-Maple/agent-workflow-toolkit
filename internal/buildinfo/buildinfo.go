// Package buildinfo exposes one version contract shared by every public CLI.
package buildinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const (
	Version  = "0.2.0"
	Maturity = "experimental"
)

type Metadata struct {
	Version  string
	Protocol int
	Maturity string
	Revision string
	Dirty    bool
}

func Current(protocol int) Metadata {
	metadata := Metadata{Version: Version, Protocol: protocol, Maturity: Maturity, Revision: "unknown"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return metadata
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if strings.TrimSpace(setting.Value) != "" {
				metadata.Revision = setting.Value
			}
		case "vcs.modified":
			metadata.Dirty = setting.Value == "true"
		}
	}
	return metadata
}

func Format(command string, protocol int) string {
	metadata := Current(protocol)
	return fmt.Sprintf("%s version=%s protocol=%d maturity=%s revision=%s dirty=%t", command, metadata.Version, metadata.Protocol, metadata.Maturity, metadata.Revision, metadata.Dirty)
}
