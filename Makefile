# Read version from VERSION file
VERSION      := $(shell cat VERSION)
VERSION_FLAT := $(shell cat VERSION | tr -d '.')

COMMANDS := check relock unlock dumpsmc hostcaps patchgos patchsmc patchvmkctl

BUILD_WIN  := build/windows
BUILD_LIN  := build/linux
BUILD_MAC  := build/macos
BUILD_DIRS := $(BUILD_WIN) $(BUILD_LIN) $(BUILD_MAC) build/iso build/templates

DIST_ZIP := dist/unlocker$(VERSION_FLAT).zip
DIST_TGZ := dist/unlocker$(VERSION_FLAT).tgz
DIST_DIR := dist/unlocker$(VERSION_FLAT)

# ----------------------------------------------------------------------------
# Default target
# ----------------------------------------------------------------------------

.PHONY: all
all: build

# ----------------------------------------------------------------------------
# Clean
# ----------------------------------------------------------------------------

.PHONY: clean
clean:
	@echo "==> Cleaning build directory"
	rm -rf build
	mkdir -p $(BUILD_DIRS)
	find commands -name "rsrc_windows_amd64.syso" -delete

# ----------------------------------------------------------------------------
# Build
# ----------------------------------------------------------------------------

.PHONY: build
build: clean version-go $(BUILD_DIRS)
	@echo "==> Building executables - $(VERSION)"
	@$(foreach cmd,$(COMMANDS),$(MAKE) --no-print-directory build-cmd CMD=$(cmd);)
	@$(MAKE) --no-print-directory assets
	@$(MAKE) --no-print-directory dist
	@echo ""
	@echo "==> Done"

$(BUILD_DIRS):
	mkdir -p $@

.PHONY: build-cmd
build-cmd:
	@echo ""
	@echo "-- $(CMD)"
	cd commands/$(CMD) && go-winres make --arch amd64 --product-version $(VERSION) --file-version $(VERSION)
	cd commands/$(CMD) && GOOS=windows GOARCH=amd64 go build -o ../../$(BUILD_WIN)/$(CMD).exe
	cd commands/$(CMD) && GOOS=linux   GOARCH=amd64 go build -o ../../$(BUILD_LIN)/$(CMD)
	cd commands/$(CMD) && GOOS=darwin  GOARCH=amd64 go build -o ../../$(BUILD_MAC)/$(CMD)
	rm -f commands/$(CMD)/rsrc_windows_amd64.syso

# ----------------------------------------------------------------------------
# Assets
# ----------------------------------------------------------------------------

.PHONY: assets
assets:
	@echo ""
	@echo "-- Copying assets"
	cp LICENSE build/
	cp *.md    build/
	cp -r cpuid/linux/*   $(BUILD_LIN)/ 2>/dev/null || true
	cp -r cpuid/windows/* $(BUILD_WIN)/ 2>/dev/null || true
	cp -r cpuid/macos/*   $(BUILD_MAC)/ 2>/dev/null || true
	cp -r iso/.           build/iso/

# ----------------------------------------------------------------------------
# Dist
# ----------------------------------------------------------------------------

.PHONY: dist
dist:
	@echo "==> Creating distribution files - $(VERSION)"
	mkdir -p dist
	rm -f  $(DIST_ZIP) $(DIST_TGZ)
	rm -rf $(DIST_DIR)
	@echo ""
	@echo "-- Creating $(DIST_ZIP)"
	7z a $(DIST_ZIP) ./build/*
	@echo ""
	@echo "-- Creating $(DIST_TGZ)"
	cd ./build && tar czvf ../$(DIST_TGZ) *
	@echo ""
	@echo "-- Extracting $(DIST_ZIP) -> $(DIST_DIR)"
	7z x -o$(DIST_DIR) $(DIST_ZIP)
	@echo ""
	@echo "-- Checksums"
	shasum -a 256 $(DIST_TGZ)
	shasum -a 256 $(DIST_ZIP)
	shasum -a 512 $(DIST_TGZ)
	shasum -a 512 $(DIST_ZIP)
	@echo ""
	@echo "==> Done"

# ----------------------------------------------------------------------------
# Version
# ----------------------------------------------------------------------------

.PHONY: version-go
version-go:
	@printf 'package vmwpatch\nconst VERSION = "%s"\n' "$(VERSION)" > vmwpatch/version.go

.PHONY: version
version:
	@echo $(VERSION)

# ----------------------------------------------------------------------------
# Help
# ----------------------------------------------------------------------------

.PHONY: help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "  all / build   Clean, then build all executables (Windows, Linux, macOS)"
	@echo "  dist          Package build output into .zip and .tgz with checksums"
	@echo "  clean         Remove build directory and recreate empty structure"
	@echo "  version       Print the current version from the VERSION file"
	@echo "  help          Show this message"
