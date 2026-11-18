BINARY  := oc-profiles
BIN_DIR := bin
# Override with: make install PREFIX=/opt/homebrew/bin
PREFIX  ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed "s/^v//" | grep . || echo 0.4.0-dev)

.PHONY: build test vet install uninstall clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN_DIR)/$(BINARY) ./cmd/oc-profiles

test:
	go test ./...

vet:
	go vet ./...

install: build
	mkdir -p $(PREFIX)
	install -m 0755 $(BIN_DIR)/$(BINARY) $(PREFIX)/$(BINARY)
	@echo "Installed $(PREFIX)/$(BINARY)"

uninstall:
	rm -f $(PREFIX)/$(BINARY)
	@echo "Removed $(PREFIX)/$(BINARY) (store, backups and opencode.json are left untouched)"

clean:
	rm -rf $(BIN_DIR)
