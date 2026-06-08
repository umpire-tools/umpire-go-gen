UMPIRE_SPEC_VERSION := v1.0.0
UMPIRE_SPEC_SHA256  := 71e7d71391e90eefe4c46c1266fbd7d57ba39f5ef72360f9a8b1c593f576fed3

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
