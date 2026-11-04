package agenticfill

import _ "embed"

// WorkerFillJS is executed by `veil agent fill run` (playwright-core must be installed beside it).
//
//go:embed worker-fill.mjs
var WorkerFillJS []byte
