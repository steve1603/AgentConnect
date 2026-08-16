# Development tasks. End users can ignore this and use run.bat / run.ps1.

BINARY := roundtable

.PHONY: all web build run test race vet fmt check clean

all: check build

## web: install frontend dependencies and build the UI into web/dist
web:
	cd web && npm install && npm run build

## build: compile the server (embeds the built UI)
build:
	go build -o $(BINARY) ./cmd/roundtable

## run: build and start the server
run: build
	./$(BINARY)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal web/embed.go

## check: everything CI should run
check: vet test
	cd web && npm run typecheck

clean:
	rm -f $(BINARY)
	rm -rf web/dist web/node_modules data
