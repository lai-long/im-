BIN := im-server

.PHONY: build run test selftest clean

build:
	go build -o $(BIN) ./cmd/im-server

run: build
	./$(BIN) -addr :7788 -data ./data

test:
	go test ./...

# 回环自测：内置 mock 接入方跑完 M1 验收全链路（CI 可用）
selftest: build
	./$(BIN) -selftest -data .

clean:
	rm -f $(BIN)
