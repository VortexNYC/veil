// Package fill ships the MV3 extension payload inside the veil binary so
// `veil fill install` can lay it down next to the native host. Runtime files
// only — fixtures and node tests stay out of the binary.
package fill

import "embed"

//go:embed manifest.json background.js tab.js content.js fields.js passkeys.js passkeys-page.js popup.html popup.js
var Files embed.FS
