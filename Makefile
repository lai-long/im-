BIN := im-server

.PHONY: build run test clean

build:
	go build -o $(BIN) ./cmd/im-server

run: build
	./$(BIN) -addr :7788 -data ./data

test:
	go test ./...

clean:
	rm -f $(BIN)
