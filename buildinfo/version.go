// Package buildinfo identifies the running Monitor binary.
package buildinfo

// Version can be injected using Go linker -X. Local builds default to dev.
var Version = "dev"
