GO ?= go

.PHONY: build test tidy run fmt vet

build:
	$(GO) build -trimpath -ldflags "-s -w" -o webserver.exe ./cmd/webserver

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

run:
	$(GO) run ./cmd/webserver -config configs/webserver.yaml
