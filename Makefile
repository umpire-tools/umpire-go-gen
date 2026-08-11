UMPIRE_SPEC_VERSION := v1.1.0-rc.2
UMPIRE_SPEC_SHA256  := 560184f267e4224f00dd5bed8a6a66bcafdb906153c80699c0f074ec30144240

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
