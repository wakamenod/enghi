# Search requires FTS5 and the trigram tokenizer, so the build tag is mandatory.
# Omitting it fails the build in cmd/enghi/require_fts5.go, as does
# CGO_ENABLED=0 in require_cgo.go.
TAGS := sqlite_fts5
BIN  := bin/enghi

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet run install clean icons

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

# Re-render the PNG and .ico icons after editing web/static/icon.svg. The
# outputs are committed, so a normal build needs neither rsvg-convert nor this.
icons:
	python3 packaging/icons/build.py
