// Package buildinfo exposes version metadata shared by CE surfaces.
package buildinfo

// Version is overridden by release builds through Go linker flags.
var Version = "0.4.0-dev"
