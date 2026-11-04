#!/bin/sh
# Ship Blume docs to veil.nyc/docs — same command cloudflare-ci runs after build.
# Normal path: push main → MirrorWorkflow → Artifacts → CI build + deployCommand.
# Use this when the page is on main but CDN still 404 (lag or failed deploy step).
set -eu
cd "$(dirname "$0")/.."
pnpm run docs:deploy
