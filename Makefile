# BHTTP/1.0 Makefile

.PHONY: all build test clean annotate serve client verify

all: build annotate test

build:
	go build -o bserve.exe ./cmd/bserve
	go build -o bcurl.exe ./cmd/bcurl
	@mkdir -p bin 2>/dev/null || true
	GOOS=linux go build -o bin/bserve ./cmd/bserve
	GOOS=linux go build -o bin/bcurl ./cmd/bcurl

annotate:
	go run ./cmd/annotate

test:
	go test -v ./...

verify: build
	@echo "Running Stranger Client Interoperability Test against SPEC.md..."
	go test -v -run TestEndToEndServerClient ./pkg/bhttp

clean:
	rm -f bserve.exe bcurl.exe

serve: build
	./bserve.exe ./www 9000

client: build
	./bcurl.exe -v localhost:9000/index.html
