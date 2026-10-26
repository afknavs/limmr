package triggers

import _ "embed"

// Schema is supplied for the hub.db migration; importing triggers applies nothing.
//
//go:embed schema.sql
var Schema string
