# FTS5 と trigram tokenizer が必要なので、**build tag は必須**(DESIGN 1)。
# 付け忘れた場合は起動時の検証(store.verifyFTS)が明示的なエラーで落とす。
TAGS := sqlite_fts5
BIN  := bin/enghi

EMACS ?= emacs
# elisp のテストは動いているサーバに対して実行する
ENGHI_TEST_URL ?= http://127.0.0.1:7799

.PHONY: build test vet run install clean test-elisp compile-elisp test-all

build:
	CGO_ENABLED=1 go build -tags $(TAGS) -o $(BIN) ./cmd/enghi

test:
	CGO_ENABLED=1 go test -tags $(TAGS) ./...

vet:
	CGO_ENABLED=1 go vet -tags $(TAGS) ./...

run: build
	./$(BIN) serve

install:
	CGO_ENABLED=1 go install -tags $(TAGS) ./cmd/enghi

# elisp のバイトコンパイル(警告を見るため)
compile-elisp:
	$(EMACS) -Q --batch -L elisp \
	  --eval '(dolist (f (file-expand-wildcards "elisp/enghi*.el")) (byte-compile-file f))'
	@rm -f elisp/*.elc

# elisp のテスト。**サーバを起動してから実行すること**:
#   ./bin/enghi serve --config <テスト用の設定> &
#   make test-elisp
test-elisp:
	ENGHI_TEST_URL=$(ENGHI_TEST_URL) $(EMACS) -Q --batch \
	  -L elisp -l elisp/enghi-tests.el -f ert-run-tests-batch-and-exit

test-all: test test-elisp

clean:
	rm -rf bin elisp/*.elc
