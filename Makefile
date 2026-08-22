.PHONY: build test lint eval eval-no-rag eval-cosine eval-score eval-compressed

build:
	go build ./...

test:
	go test ./... -count=1

lint:
	golangci-lint run ./...

# Full 4-condition eval (increase cooldown if YUKI GPU throttles)
eval:
	go run ./cmd/llmo/ eval -cases cases -out results -cooldown 10

# Single conditions — run separately to avoid thermal shutdown on large models
eval-no-rag:
	go run ./cmd/llmo/ eval -cases cases -out results -conditions no-rag -cooldown 5

eval-cosine:
	go run ./cmd/llmo/ eval -cases cases -out results -conditions cosine -cooldown 10

eval-score:
	go run ./cmd/llmo/ eval -cases cases -out results -conditions score -cooldown 10

eval-compressed:
	go run ./cmd/llmo/ eval -cases cases -out results -conditions compressed -cooldown 10
