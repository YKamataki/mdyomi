.PHONY: check

check:
	@files=$$(gofmt -l *.go) && { test -z "$$files" || { echo "Run gofmt on: $$files"; exit 1; }; }
	go mod download
	go mod verify
	go vet ./...
	@parser_dir=$$(go list -m -f '{{.Dir}}' github.com/yuin/goldmark) && \
		MDYOMI_COMMONMARK_SPEC="$$parser_dir/_test/spec.json" go test -count=1 ./...
