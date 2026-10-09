// Package docs holds the user documentation (Markdown), embedded in the binary for the studio's
// MCP server (agents read it as resources).
package docs

import "embed"

//go:embed *.md background/*.md lyrics/*.md particles/*.md visualizer/*.md technical/*.md
var FS embed.FS
