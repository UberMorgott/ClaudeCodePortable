BINARY := ccp.exe
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
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY) $(PKG)
	upx --best --lzma dist/$(BINARY)
	cd dist && sha256sum --text $(BINARY) > checksums.txt
