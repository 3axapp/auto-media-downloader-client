.PHONY: test vet build-linux build-windows dist

test:
	go test ./...

vet:
	go vet ./...
	GOOS=windows go vet ./...

build-linux:
	CGO_ENABLED=0 go build -trimpath -o dist/amd-client ./cmd/amd-client

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
		-ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty)" \
		-o dist/amd-client.exe ./cmd/amd-client

dist: test vet build-windows
	@echo "готово: dist/amd-client.exe"
