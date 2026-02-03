.PHONY: build test clean run

BINARY_NAME=heisenberg

build:
	go build -o $(BINARY_NAME) .

run: build
	./$(BINARY_NAME) $(ARGS)

test:
	go test -v ./...

clean:
	rm -f $(BINARY_NAME)
