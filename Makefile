.PHONY: test vet fmt tidy sdk-fresh ci build identity-config identity-env identity-up glue prove-identity prove-hydra prove-cli-golden-flow prove-live prove-fill loadtest

test:
	env -u VEIL_HYDRA_ISSUER -u VEIL_HYDRA_ADMIN -u VEIL_HOME -u VEIL_OIDC_TOKEN -u VEIL_ORIGIN -u VEIL_OIDC_TOKEN_FILE -u VEIL_HYDRA_SECRET_FILE -u VEIL_AGENT VEIL_FILL_TOUCHID=0 go test -race -shuffle=on -timeout 15m ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

build:
	go build -o bin/veil ./cmd/veil

sdk-fresh:
	pnpm run sdk:generate
	@if [ -n "$$(git status --porcelain -- sdks internal/publicapi/spec.json)" ]; then \
		echo "sdk:generate dirtied committed SDKs. Commit the generator output or revert the OpenAPI change."; \
		git status -- sdks internal/publicapi/spec.json; \
		git diff -- sdks internal/publicapi/spec.json; \
		exit 1; \
	fi

ci:
	$(MAKE) sdk-fresh
	$(MAKE) vet test
	pnpm exec vp lint
	pnpm run typecheck
	pnpm --filter veil-vault test
	pnpm run docs:build

prove-cli-golden-flow:
	go test ./internal/cli ./internal/mcpserver ./internal/publicapi -count=1

identity-config:
	docker compose --env-file identity/.env -f identity/compose.yml config

identity-env:
	@test -f identity/.env || { \
		printf '%s\n' \
			"POSTGRES_PASSWORD=$$(openssl rand -hex 24)" \
			"KRATOS_COOKIE_SECRET=$$(openssl rand -hex 24)" \
			"KRATOS_CIPHER_SECRET=$$(openssl rand -hex 16)" \
			"HYDRA_SYSTEM_SECRET=$$(openssl rand -hex 24)" \
			"HYDRA_PAIRWISE_SALT=$$(openssl rand -hex 24)" \
			"HYDRA_CLIENT_ID=veil" \
			"BROKER_REDIRECT_URL=http://127.0.0.1:4460/oidc/callback" \
			"URLS_SELF_ISSUER=http://127.0.0.1:4444" \
			> identity/.env; \
	}

identity-up: identity-env
	docker compose --env-file identity/.env -f identity/compose.yml up -d

prove-identity: identity-up
	go test -tags live ./identity/glue -count=1 -timeout 3m

# Real-Hydra proof: a self-issued dev Hydra (issuer=itself, unlike the
# identity-hydra compose service which masquerades as id.veil.nyc) mints a
# real id_token; production code verifies it via JWKS, then a live app test
# runs provision → item → agent → grant → use on real Postgres.
prove-hydra:
	@docker start veil-pg-test 2>/dev/null || docker run -d --name veil-pg-test \
	  -p 127.0.0.1:55432:5432 -e POSTGRES_PASSWORD=test -e POSTGRES_DB=veiltest postgres:17
	@docker start veil-hydra-test 2>/dev/null || docker run -d --name veil-hydra-test \
	  -p 127.0.0.1:5555:4444 -p 127.0.0.1:5556:4445 \
	  -e DSN=memory -e SECRETS_SYSTEM=test-system-secret-0123456789abcdef \
	  -e OIDC_SUBJECT_IDENTIFIERS_PAIRWISE_SALT=test-salt-0123456789abcdef \
	  -e URLS_SELF_ISSUER=http://127.0.0.1:5555 \
	  oryd/hydra:v26.2.0 serve all --dev
	@for i in $$(seq 1 50); do curl -sf http://127.0.0.1:5555/health/ready >/dev/null && break || sleep 0.2; done
	@for i in $$(seq 1 50); do docker exec veil-pg-test pg_isready -U postgres >/dev/null 2>&1 && break || sleep 0.2; done
	PG_TEST_DSN="postgres://postgres:test@127.0.0.1:55432/veiltest?sslmode=disable" \
	  HYDRA_TEST_PUBLIC=http://127.0.0.1:5555 HYDRA_TEST_ADMIN=http://127.0.0.1:5556 \
	  go test -tags live -p 1 ./internal/human ./internal/app -run HydraLive -count=1 -v

prove-live:
	./scripts/prove-live.sh

prove-fill:
	./scripts/prove-fill.sh

login:
	pnpm --filter identity-login dev

glue:
	go run ./cmd/identity-glue

loadtest:
	@if ! command -v k6 >/dev/null 2>&1; then \
		echo "k6 not installed. Install from https://grafana.com/docs/k6/latest/set-up/install-k6/"; \
		exit 1; \
	fi
	@test -n "$$VEIL_AGENT_TOKEN" || { echo "VEIL_AGENT_TOKEN is required"; exit 1; }
	@test -n "$$VEIL_ITEM_ID" || { echo "VEIL_ITEM_ID is required"; exit 1; }
	k6 run tests/load/k6/use.js
