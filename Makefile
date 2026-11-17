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

.PHONY: api
api: ## Write the public API listing api/ginject.txt
	go run ./internal/apicheck -write

.PHONY: api-check
api-check: ## Fail when the public API differs from api/ginject.txt
	go run ./internal/apicheck

.PHONY: check
check: ## Everything CI runs: format, vet, API, lint, tests
	@test -z "$$(gofmt -l $$(git ls-files '*.go'))" || (gofmt -l $$(git ls-files '*.go'); echo "run make fmt"; exit 1)
	go vet ./...
	$(MAKE) api-check
	golangci-lint run
	go test ./... -cover -race
	go test ./core -run '^TestTrace_(NoListenerGeneratesOneID|ListenerGeneratesOneIDPerStage)$$' -count 1
	go test ./middlewares/loadshed -run '^TestAdaptive_(Stable|NoRefusalBelowCapacity|RecoversWhenHandlerSlows|Steering|LongHeld)_' -count 1 -p 1

BENCH_COUNT ?= 5

.PHONY: bench
bench: ## End-to-end HTTP benchmark of ginject (bench/), median of BENCH_COUNT runs
	cd bench && go test -run '^$$' -bench 'BenchmarkGinject' -benchmem -count $(BENCH_COUNT) ./...

.PHONY: bench-compare
bench-compare: ## Same scenarios on ginject, net/http, Gin, Echo, Chi and Fiber (bench/)
	cd bench && go test -run '^$$' -bench . -benchmem -count $(BENCH_COUNT) ./...

.PHONY: load
load: ## Load test with wrk on a release build (bench/load.sh); FRAMEWORKS="ginject gin" LOGS="on off" PROCS="1 2 all" DURATION=10s CONNECTIONS="64 256"
	./bench/load.sh
