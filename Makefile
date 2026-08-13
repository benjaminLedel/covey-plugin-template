# The build is REPRODUCIBLE on purpose: -trimpath keeps the build path out of
# the binary, so the same source and the same Go version produce the same bytes
# on any machine. That is what lets the catalogue's CI rebuild your module and
# prove it is the source you published — without it, "we reviewed the code"
# says nothing about the binary anybody installs.
PLUGIN ?= example
GOFLAGS_REPRO := -trimpath

.PHONY: build lint digest test clean

build:
	GOOS=wasip1 GOARCH=wasm go build $(GOFLAGS_REPRO) -o $(PLUGIN).wasm .
	@echo
	@echo "  $(PLUGIN).wasm  $$(wc -c < $(PLUGIN).wasm) bytes"
	@echo "  sha256: $$(shasum -a 256 $(PLUGIN).wasm | cut -d' ' -f1)"
	@echo "  go:     $$(go version | awk '{print $$3}' | sed 's/^go//')"
	@echo
	@echo "Put the digest and the Go version in your catalogue entry."

# The same check Covey runs when somebody installs your plugin.
lint: build
	covey plugin lint $(PLUGIN).wasm

digest: build
	@shasum -a 256 $(PLUGIN).wasm | cut -d' ' -f1

test:
	go test ./...

clean:
	rm -f $(PLUGIN).wasm
