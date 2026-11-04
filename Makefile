GO := go

# Version number of linter
# Built by go install through the Go module proxy, so no release download is needed
LINT_VERSION ?= 2.15.0

# Relative file path of the binary file of linter
LINT_BIN ?= ./bin/golangci-lint

# True if required linter version is installed, false otherwise
IS_LINT_INSTALLED := $(shell $(LINT_BIN) version 2> /dev/null | grep $(LINT_VERSION))

# Coverage profile of the test target; removed after the run, even a failed one
COVER_FILE := .test-coverage

# Wails builds, packages and the AppImage check live in Taskfile.yml (task 3.54.0); these targets call it
TASK ?= task

# Cargo of the Rust search worker in search/; the toolchain comes from search/rust-toolchain.toml
CARGO ?= cargo

# SEARCH_MODEL=1 runs the search tests marked #[ignore] too: they need the model and ONNX Runtime
# fetched into build/model and build/ort (task model:fetch, task ort:fetch)
ifdef SEARCH_MODEL
SEARCH_MODEL_ENV := SMARTNOTE_MODEL_DIR=$(CURDIR)/build/model SMARTNOTE_ORT_LIB=$(firstword $(wildcard $(CURDIR)/build/ort/libonnxruntime.*))
SEARCH_MODEL_ARGS := -- --include-ignored
endif

help: ## Display this help screen
	@echo "Makefile available targets:"
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  * \033[36m%-17s\033[0m %s\n", $$1, $$2}'

all: dep test lint ## Run default complete set of commands

dep: ## Sync dependencies
	@echo "+ $@"
	$(GO) mod tidy

frontend: ## Build frontend/dist, which the Go code embeds and needs to compile
	@echo "+ $@"
	$(TASK) common:build:frontend

test: frontend ## Run tests with the race detector (needs a C compiler) and print total coverage
	@echo "+ $@"
	@trap 'rm -f $(COVER_FILE)' EXIT; \
	$(GO) test -race -failfast -timeout 300s -coverprofile=$(COVER_FILE) ./... && \
	$(GO) tool cover -func=$(COVER_FILE) | tail -n1 | awk '{print "Total test coverage: " $$3}'

integration: frontend ## Run the integration tests against PostgreSQL and OpenSSH in Docker
	@echo "+ $@"
	$(GO) test -race -timeout 600s -tags integration ./...

lint: install-lint frontend ## Lint the source files and check vulnerabilities
	@echo "+ $@"
	@$(LINT_BIN) run --timeout=5m
	$(GO) tool govulncheck ./...

lint-fix: install-lint frontend ## Apply automatic lint fixes and check vulnerabilities
	@echo "+ $@"
	@$(LINT_BIN) run --timeout=5m --fix
	$(GO) tool govulncheck ./...

fmt: install-lint ## Format the source files
	@echo "+ $@"
	@$(LINT_BIN) fmt

install-lint: ## Install linter
ifndef IS_LINT_INSTALLED
	@echo "+ $@"
	GOBIN=$(abspath $(dir $(LINT_BIN))) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(LINT_VERSION)
endif

# The dump test runs again with the release profile (opt-level 3, lto thin, panic abort) of the
# shipped worker: dropping the zeroing as a dead store shows only in optimised code.
# ORT_DISABLE_TELEMETRY=1 keeps ONNX Runtime loaded into a test process from starting its
# telemetry client; the worker sets it itself, and the tests that start it do not pass it on.
search-test: ## Run the tests of the Rust search worker, with the probe binaries (needs gdb, strace; SEARCH_MODEL=1 adds the model tests)
	@echo "+ $@"
	cd search && ORT_DISABLE_TELEMETRY=1 $(SEARCH_MODEL_ENV) $(CARGO) test --workspace --features test-probes $(SEARCH_MODEL_ARGS)
	cd search && ORT_DISABLE_TELEMETRY=1 $(CARGO) test --release -p worker --features test-probes --test zalloc_dump

# Benchmark of the search (spec §12.4): the release worker and the notes of the benchmark set
SEARCH_TARGET_DIR := $(or $(CARGO_TARGET_DIR),$(CURDIR)/search/target)
BENCH_CORPUS ?= $(SEARCH_TARGET_DIR)/bench-corpus.json

search-bench: ## Measure search quality and criterion 4 on the benchmark set (needs the model; takes minutes, keep the machine idle)
	@echo "+ $@"
	cd search && ORT_DISABLE_TELEMETRY=1 SMARTNOTE_BENCH=sweep SMARTNOTE_MODEL_DIR=$(CURDIR)/build/model SMARTNOTE_ORT_LIB=$(firstword $(wildcard $(CURDIR)/build/ort/libonnxruntime.*)) \
		$(CARGO) test --release -p worker --test bench -- --ignored --nocapture
	cd search && $(CARGO) build --release -p worker --bin smartnote-search
	cd search && $(CARGO) run --release -p worker --example bench_corpus -- $(BENCH_CORPUS)
	SMARTNOTE_SEARCH_BIN=$(SEARCH_TARGET_DIR)/release/smartnote-search SMARTNOTE_BENCH_CORPUS=$(BENCH_CORPUS) \
	SMARTNOTE_MODEL_DIR=$(CURDIR)/build/model SMARTNOTE_ORT_LIB=$(firstword $(wildcard $(CURDIR)/build/ort/libonnxruntime.*)) \
		$(GO) test -tags integration -run '^$$' -bench BenchmarkSearchService -benchtime 2000x -timeout 60m -v ./internal/adapter/wailsui

# The unsafe code (the zeroing allocator, the hardening and the sandbox of the worker) under the
# address sanitizer (spec §12.1). Needs a nightly toolchain (rustup toolchain install nightly);
# --target keeps build scripts and proc macros out of the sanitizer. Not part of CI.
SEARCH_ASAN_TARGET ?= x86_64-unknown-linux-gnu

search-asan: ## Run the unsafe code of the Rust search worker under AddressSanitizer (needs nightly)
	@echo "+ $@"
	cd search && RUSTUP_TOOLCHAIN=nightly RUSTFLAGS=-Zsanitizer=address RUSTDOCFLAGS=-Zsanitizer=address \
		$(CARGO) test --locked --target $(SEARCH_ASAN_TARGET) -p zalloc
	cd search && RUSTUP_TOOLCHAIN=nightly RUSTFLAGS=-Zsanitizer=address ORT_DISABLE_TELEMETRY=1 \
		$(CARGO) test --locked --target $(SEARCH_ASAN_TARGET) -p worker --lib

search-lint: ## Check format, clippy and cargo-deny policy of the Rust search worker
	@echo "+ $@"
	cd search && $(CARGO) fmt --check
	cd search && $(CARGO) clippy --workspace --all-targets --all-features -- -D warnings
	cd search && $(CARGO) deny check

build: ## Build the application to ./bin (task build)
	@echo "+ $@"
	$(TASK) build

package: ## Build the .deb package and the AppImage to ./bin (task package:linux)
	@echo "+ $@"
	$(TASK) package:linux

smoke: ## Start the AppImage in a clean ubuntu:26.04 container (task smoke:appimage)
	@echo "+ $@"
	$(TASK) smoke:appimage

.PHONY: help all dep frontend test integration lint lint-fix fmt install-lint search-test search-asan search-bench search-lint build package smoke
