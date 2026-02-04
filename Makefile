.PHONY: build test clean run lint setup

BINARY_NAME=heisenberg

build:
	go build -o $(BINARY_NAME) .

run: build
	./$(BINARY_NAME) $(ARGS)

test:
	go test -race -v ./...

lint:
	go vet ./...
	golangci-lint run --timeout=5m

setup:
	brew install lefthook golangci-lint
	lefthook install

clean:
	rm -f $(BINARY_NAME)
