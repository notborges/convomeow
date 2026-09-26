.PHONY: fmt fmt-check check web-check

fmt:
	gofmt -w cmd internal
	cd web && bun run format

fmt-check:
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Files need gofmt:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

web-check:
	cd web && bun run check

check: fmt-check web-check
	go mod tidy -diff
	go mod verify
	go build ./...
	go vet ./...
	go test -race ./...
