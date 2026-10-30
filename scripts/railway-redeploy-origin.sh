#!/bin/sh
# Redeploy origin from GitHub main — never `railway up` from a local checkout
# (that path has caused wrong-service deploys, including Postgres once).
set -eu
svc="${1:-veil}"
if [ "$svc" = "Postgres" ] || [ "$svc" = "postgres" ]; then
	echo "refusing: Postgres must stay on the postgres-ssl image; use docs/postgres-creds-runbook.md" >&2
	exit 1
fi
railway service link "$svc"
railway redeploy --service "$svc" --from-source -y
railway deployment list --service "$svc" | head -5
