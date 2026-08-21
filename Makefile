.PHONY: build test install
build:
	go build -ldflags "-X github.com/allen-hsu/gpc/cmd.Version=$$(git describe --tags --always --dirty)" -o bin/gpc .
test:
	go vet ./... && go test ./...
install:
	go install -ldflags "-X github.com/allen-hsu/gpc/cmd.Version=$$(git describe --tags --always --dirty)" .
