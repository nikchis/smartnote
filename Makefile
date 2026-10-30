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

build: ## Build the application to ./bin (task build)
	@echo "+ $@"
	$(TASK) build

package: ## Build the .deb package and the AppImage to ./bin (task package:linux)
	@echo "+ $@"
	$(TASK) package:linux

smoke: ## Start the AppImage in a clean ubuntu:26.04 container (task smoke:appimage)
	@echo "+ $@"
	$(TASK) smoke:appimage

.PHONY: help all dep frontend test integration lint lint-fix fmt install-lint build package smoke
