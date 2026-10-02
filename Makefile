BINARY_NAME := asana-extractor
CMD_PATH    := ./cmd/backednsvc
BIN_DIR     := bin

.PHONY: build run run-5m run-30s test vet fmt clean mocks e2e lint

build:
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)

run:
	go run $(CMD_PATH)

run-5m:
	go run $(CMD_PATH) --interval=5m

run-30s:
	go run $(CMD_PATH) --interval=30s

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

clean:
	rm -rf $(BIN_DIR) output

mocks:
	go run github.com/vektra/mockery/v2@v2.53.7

e2e:
	./scripts/e2e-test.sh

lint:
	golangci-lint run
