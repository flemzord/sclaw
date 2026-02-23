.PHONY: build lint test release-snapshot clean

build:
	go build -o sclaw ./cmd/sclaw

lint:
	golangci-lint run

test:
	go test -race -coverprofile=coverage.txt ./...

release-snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f sclaw coverage.txt
	rm -rf dist/
