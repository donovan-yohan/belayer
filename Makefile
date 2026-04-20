# Belayer dev tooling. Unified entrypoints for both the Go daemon and the
# Python bridge. Run `make help` for a target list.

.PHONY: help build test test-go test-python lint vet sync clean

help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the belayer binary
	go build -o belayer ./cmd/belayer

test: test-go test-python ## Run Go + Python test suites

test-go: ## Run Go test suite
	go test ./...

test-python: sync ## Run Python bridge test suite via uv
	uv run pytest

sync: ## Ensure .venv is in sync with pyproject.toml
	uv sync

vet: ## Run go vet
	go vet ./...

lint: vet ## Alias for vet (extend when we add ruff/mypy)

clean: ## Remove build artifacts and caches
	rm -f belayer
	rm -rf .venv .pytest_cache
	find . -type d -name __pycache__ -prune -exec rm -rf {} +
	find . -type f -name '*.pyc' -delete
