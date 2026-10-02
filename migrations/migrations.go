// Package migrations embeds controlled SQL steps; hosts never run them implicitly.
package migrations

import "embed"

//go:embed postgres/*.sql sqlite/*.sql
var Files embed.FS
