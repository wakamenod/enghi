# FTS5 と trigram tokenizer が必要なので、**build tag は必須**(DESIGN 1)。
# 付け忘れた場合は起動時の検証(store.verifyFTS)が明示的なエラーで落とす。
TAGS := sqlite_fts5
BIN  := bin/enghi

# タグの無いビルドは cmd/enghi/require_fts5.go がコンパイルエラーで止める。
# CGO_ENABLED=0 も同様(require_cgo.go)。
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet run install clean

build:
	CGO_ENABLED=1 go build -tags $(TAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/enghi

test:
	CGO_ENABLED=1 go test -tags $(TAGS) ./...

vet:
	CGO_ENABLED=1 go vet -tags $(TAGS) ./...

run: build
	./$(BIN) serve

install:
	CGO_ENABLED=1 go install -tags $(TAGS) -ldflags "$(LDFLAGS)" ./cmd/enghi

clean:
	rm -rf bin
