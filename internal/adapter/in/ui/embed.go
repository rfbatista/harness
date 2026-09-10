package ui

import "embed"

// Dist embeds the production UI. Populate before go build by copying web/dist:
//
//	cp -r web/dist/* internal/adapter/in/ui/static/
//
// Or run: cd web && npm run build && cp -r dist/* ../internal/adapter/in/ui/static/
//
//go:embed static
var Dist embed.FS
