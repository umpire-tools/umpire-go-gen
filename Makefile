UMPIRE_SPEC_VERSION := v1.1.0-rc.1
UMPIRE_SPEC_SHA256  := 39062cc8ee33c19fd260471374cc9c410e407c51a47650a8d2824efec6b70d69

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
