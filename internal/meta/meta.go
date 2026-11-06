// Package meta holds build-time identity: the tool name, version and the mandatory disclaimer.
package meta

// Name is the tool name.
const Name = "reservelint"

// Disclaimer must appear in every report, the README and the CLI help.
const Disclaimer = "Monitoring and reporting aid. Not legal advice. Not an official OCC form."

// Version is overridden at build time:
//
//	go build -ldflags "-X github.com/jegorscode/reservelint/internal/meta.Version=1.2.3"
var Version = "0.1.0-dev"
