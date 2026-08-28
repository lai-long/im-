BIN := im-server
WEB := web

.PHONY: build web-install web-build run test selftest clean

web-install:
	cd $(WEB) && npm install

# 前端构建（Vite + tsc）；node_modules 缺失时自动 install
web-build:
	cd $(WEB) && ([ -d node_modules ] || npm install) && npm run build

build: web-build
	go build -o $(BIN) ./cmd/im-server

run: build
	./$(BIN) -addr :7788 -data ./data

test: web-build
	go test ./...

# 回环自测：内置 mock 接入方跑完 M1+M2+M3 验收全链路（CI 可用）
selftest: build
	./$(BIN) -selftest -data .

clean:
	rm -f $(BIN)
	cd $(WEB) && rm -rf dist
