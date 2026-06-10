// Package dq exposes the repository README as an embedded guide
// so the CLI can print it via the -agent-guide flag.
package dq

import _ "embed"

//go:embed README.md
var AgentGuide string
