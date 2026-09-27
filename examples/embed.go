// Package examples embeds the sample workflows shown in the web panel.
package examples

import "embed"

//go:embed */workflow.json
var FS embed.FS
