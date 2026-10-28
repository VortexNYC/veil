#!/bin/sh
# deploy-check.sh — post-deploy smoke. Probes every public surface and
# pages via ntfy when one stops answering. A deploy that ships a dead
# route is invisible until this runs — the backup-ingest 404 outage
# (2026-10-11) was eleven days because nothing looked.
#
#   sh scripts/deploy-check.sh            # probe all
#   sh scripts/deploy-check.sh backup     # one surface
#
# Expected codes are honest per-surface: POST-only workers answer 405,
# the consent API answers 400 on a bare GET — a 200-only check would
# page on healthy services.
set -u

NTFY="${VEIL_NTFY_TOPIC:-}"
fail=0

probe() { # <name> <url> <expected-code> [body-match]
	name=$1 url=$2 want=$3 match=${4:-}
	out=$(curl -sS -o /tmp/dc-body.$$ -w '%{http_code}' --max-time 10 "$url" 2>/dev/null || echo "000")
	if [ "$out" != "$want" ]; then
		echo "FAIL $name $url -> $out (want $want)"
		fail=1
	elif [ -n "$match" ] && ! grep -q "$match" /tmp/dc-body.$$; then
		echo "FAIL $name $url body missing '$match'"
		fail=1
	else
		echo "ok   $name $url -> $out"
	fi
	rm -f /tmp/dc-body.$$
}

case "${1:-all}" in
backup|ingest)
	probe backup-ingest "https://backup-ingest.veil.nyc/healthz" 200 "ok"
	;;
origin)
	probe origin "https://veil.nyc/ready" 200 "ok errors_5m="
	;;
*)
	probe origin "https://veil.nyc/ready" 200 "ok errors_5m="
	probe backup-ingest "https://backup-ingest.veil.nyc/healthz" 200 "ok"
	probe login "https://login.veil.nyc/" 200
	probe vault "https://app.veil.nyc/" 200
	probe hydra "https://id.veil.nyc/.well-known/openid-configuration" 200 "issuer"
	probe kratos "https://accounts.veil.nyc/health/ready" 200
	probe consent "https://consent.veil.nyc/" 400
	probe mail "https://mail.veil.nyc/" 405
	probe keys "https://keys.veil.nyc/health" 200 "veil-key-service"
	;;
esac

if [ "$fail" = 1 ]; then
	if [ -n "$NTFY" ]; then
		curl -sS -o /dev/null -X POST "https://ntfy.sh/$NTFY" \
			-H "Title: veil deploy-check failed" -H "Priority: high" -H "Tags: warning" \
			-d "a public surface stopped answering after a deploy — see deploy-check.sh output" || true
	fi
	exit 1
fi
echo "deploy-check: all surfaces ok"
origin_ready=$(curl -sS --max-time 5 "https://veil.nyc/ready" 2>/dev/null | tr -d '\n')
main_sha=$(git rev-parse --short origin/main 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo "")
if [ -n "$origin_ready" ]; then
	echo "info origin /ready: $origin_ready (origin/main=$main_sha)"
fi
