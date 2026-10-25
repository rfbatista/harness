package domain

import _ "embed"

// ArchitectureSchemaJSON is the JSON Schema (2020-12) of ArchitectureRecord,
// served at /api/architecture.schema.json and written next to the exported
// file.
//
//go:embed architecture.schema.json
var ArchitectureSchemaJSON []byte
