// Package webui embeds the static WebUI (3.x look, API v1).
package webui

import "embed"

//go:embed static
var Files embed.FS
