#!/bin/sh
# Pre-cohort gate — same bar as make prove-live.
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
exec sh scripts/prove-live.sh
