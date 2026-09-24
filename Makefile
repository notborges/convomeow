.PHONY: fmt fmt-check check

fmt:
	gofmt -w cmd internal

fmt-check:
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Files need gofmt:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

check: fmt-check
	go mod tidy -diff
	go mod verify
	go build ./...
	go vet ./...
	go test -race ./...
