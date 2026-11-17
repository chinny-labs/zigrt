# Zigrt tasks. `just` lists them.
set dotenv-load := false

pg_url := env_var_or_default("ZIGRT_TEST_POSTGRES_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")

default:
    @just --list

# Run the app locally on :8080 with a SQLite file in ./data (plain HTTP).
run:
    ZIGRT_DATABASE_URL=sqlite://./data/zigrt.db ZIGRT_INSECURE_COOKIES=true ZIGRT_LOG_FORMAT=text go run ./cmd/zigrt

# Build the binary into ./bin.
build:
    CGO_ENABLED=0 go build -trimpath -o bin/zigrt ./cmd/zigrt

# Unit tests plus the store contract suite on SQLite.
test:
    go test -race ./...

# The content authoring tool, e.g. `just content check` on the prototype pack.
content *args:
    go run ./cmd/zigrt-content -pack content/testdata/hardest {{args}}

# The store contract suite on Postgres (needs a server; see pg_url).
test-pg:
    ZIGRT_TEST_POSTGRES_URL='{{pg_url}}' go test -race ./internal/store/...

# Start a throwaway Postgres 16 in Docker for test-pg.
pg-up:
    docker run -d --rm --name zigrt-pg -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:16

# gofmt, vet, golangci-lint and migration parity.
lint:
    test -z "$(gofmt -l .)"
    go vet ./...
    golangci-lint run ./...
    scripts/check-migrations.sh

# Build the image with the Dockerfile.
image tag="zigrt:dev":
    docker build -t {{tag}} --build-arg COMMIT=$(git rev-parse HEAD) .

# Multi-arch image build with buildx (no push), as the release does it.
image-multiarch:
    docker buildx build --platform linux/amd64,linux/arm64 --build-arg COMMIT=$(git rev-parse HEAD) .

# Lint and render the Helm chart.
helm:
    helm lint deploy/helm/zigrt --strict
    helm template zg deploy/helm/zigrt > /dev/null

# Snapshot the local SQLite database (./data) now.
backup:
    ZIGRT_DATABASE_URL=sqlite://./data/zigrt.db go run ./cmd/zigrt backup

# Build the image, run it, and drive the Playwright smoke test against it.
e2e:
    cd e2e && npm ci
    e2e/run-image.sh
