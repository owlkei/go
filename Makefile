.PHONY: help
help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' ${MAKEFILE_LIST} \
	| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: deps
deps: ## Download dependencies
	yarn
	go mod download

.PHONY: upgrade
upgrade: ## Upgrade dependencies
	go get -u ./...

.PHONY: tidy
tidy: ## Go mod tidy
	go mod tidy
	
.PHONY: test
test: ## Run tests
	go test ./... -cover -race

.PHONY: lint
lint: ## Run linter
	golangci-lint run

.PHONY: fmt
fmt: ## Format Go sources
	gofmt -w $$(git ls-files '*.go')

.PHONY: check
check: ## Everything CI runs: format, vet, lint, tests
	@test -z "$$(gofmt -l $$(git ls-files '*.go'))" || (gofmt -l $$(git ls-files '*.go'); echo "run make fmt"; exit 1)
	go vet ./...
	golangci-lint run
	go test ./... -cover -race

BENCH_COUNT ?= 5

.PHONY: bench
bench: ## End-to-end HTTP benchmark of ginject (bench/), median of BENCH_COUNT runs
	cd bench && go test -run '^$$' -bench 'BenchmarkGinject' -benchmem -count $(BENCH_COUNT) ./...

.PHONY: bench-compare
bench-compare: ## Same scenarios on ginject, net/http, Gin, Echo, Chi and Fiber (bench/)
	cd bench && go test -run '^$$' -bench . -benchmem -count $(BENCH_COUNT) ./...
