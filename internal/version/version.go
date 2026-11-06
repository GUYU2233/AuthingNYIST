// Package version exposes build metadata shared by terminal entry points.
package version

// Version is set by release builds using -ldflags -X.
// A source build deliberately does not claim to be a published release.
var Version = "dev"
