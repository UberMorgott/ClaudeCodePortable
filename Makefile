BINARY := ccp.exe
# Release asset name go-selfupdate's matcher resolves by OS/arch.
RELEASE_BINARY := ccp_windows_amd64.exe
PKG := ./cmd/ccp
VERSION ?= dev
LDFLAGS := -s -w -X github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo.Version=$(VERSION)

.PHONY: build release vet fmt clean
build:
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) $(PKG)
vet:
	go vet ./...
fmt:
	gofmt -w .
clean:
	rm -rf dist $(BINARY)
release:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(RELEASE_BINARY) $(PKG)
	upx --best --lzma dist/$(RELEASE_BINARY)
	cd dist && sha256sum --text $(RELEASE_BINARY) > checksums.txt
