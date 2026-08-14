UMPIRE_SPEC_VERSION := v1.1.0-rc.3
UMPIRE_SPEC_SHA256  := 392aabcd711ab26df3a1e90b0789a23faf875a922c955445e05f3ccadff75d1c

spec/.synced-at-version: Makefile
	@tarball=$$(mktemp -t umpire-spec-XXXXXX.tar.gz); \
	curl -fsSL "https://github.com/umpire-tools/umpire-spec/archive/refs/tags/$(UMPIRE_SPEC_VERSION).tar.gz" -o "$$tarball"; \
	echo "$(UMPIRE_SPEC_SHA256)  $$tarball" | shasum -a 256 -c -; \
	rm -rf spec; mkdir -p spec; \
	tar -xzf "$$tarball" -C spec --strip-components=1; \
	echo "$(UMPIRE_SPEC_VERSION)" > spec/.synced-at-version; \
	rm "$$tarball"

.PHONY: test
test: spec/.synced-at-version
	go test -v ./...

.PHONY: spec-sync
spec-sync: spec/.synced-at-version
