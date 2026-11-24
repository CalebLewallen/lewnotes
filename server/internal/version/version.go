// Package version carries the build version of lewnotes-core.
package version

// Version is overridden at build time with
// -ldflags "-X github.com/caleblewallen/lewnotes/server/internal/version.Version=<version>".
var Version = "dev"
