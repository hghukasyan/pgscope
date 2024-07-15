MODULE := github.com/hghukasyan/pgscope
BIN := bin/pgscope
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)

.PHONY: all build test race vet fmt tidy integration docker-up docker-down cli-help screenshots clean

all: vet test build

build:
	mkdir -p bin
	go build -ldflags "-X github.com/hghukasyan/pgscope/internal/cli.Version=$(VERSION)" -o $(BIN) ./cmd/pgscope

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

tidy:
	go mod tidy

integration: docker-up
	DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable' go test ./test/integration/ -count=1 -v

docker-up:
	docker compose -f deployments/docker-compose.yml up -d --wait

docker-down:
	docker compose -f deployments/docker-compose.yml down -v

cli-help: build
	./$(BIN) --help
	./$(BIN) version

# Regenerate SVG screenshots from a live DATABASE_URL (demo only).
screenshots: build
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL required" && exit 1)
	@mkdir -p assets/screenshots/raw
	./$(BIN) scan --no-color > assets/screenshots/raw/overview.txt
	./$(BIN) queries top --limit 8 --no-color > assets/screenshots/raw/queries.txt
	{ ./$(BIN) indexes inspect --no-color; echo; ./$(BIN) indexes duplicates --no-color; } > assets/screenshots/raw/indexes.txt
	{ ./$(BIN) health --no-color; echo; ./$(BIN) vacuum risks --no-color | head -25; } > assets/screenshots/raw/health.txt
	python3 scripts/render_terminal_svg.py assets/screenshots/raw/overview.txt assets/screenshots/overview.svg --title 'pgscope scan'
	python3 scripts/render_terminal_svg.py assets/screenshots/raw/queries.txt assets/screenshots/queries.svg --title 'pgscope queries top'
	python3 scripts/render_terminal_svg.py assets/screenshots/raw/indexes.txt assets/screenshots/indexes.svg --title 'pgscope indexes'
	python3 scripts/render_terminal_svg.py assets/screenshots/raw/health.txt assets/screenshots/health.svg --title 'pgscope health / vacuum'

clean:
	rm -rf bin
