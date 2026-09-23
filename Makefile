BINARY_NAME=avdslim
BUILD_DIR=bin
VERSION=1.0.15
LDFLAGS=-s -w -X main.version=$(VERSION)

.PHONY: all build clean cross install test

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/avdslim
	@echo "✓ Built $(BUILD_DIR)/$(BINARY_NAME)"

cross:
	@mkdir -p $(BUILD_DIR)
	@echo "--> Building macOS ARM64..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/avdslim
	@echo "--> Building macOS Intel..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/avdslim
	@echo "--> Building Linux x86_64..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/avdslim
	@echo "--> Building Windows x86_64..."
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/avdslim
	@echo "✓ Cross-compilation complete! Artifacts in $(BUILD_DIR)/"

install: build
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME) 2>/dev/null || \
	 cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME) 2>/dev/null || \
	 echo "Could not copy to /usr/local/bin. Manually copy $(BUILD_DIR)/$(BINARY_NAME) to your PATH."
	@echo "✓ Installed $(BINARY_NAME)"

clean:
	rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned $(BUILD_DIR)"

test:
	go test ./...
