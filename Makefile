PROJECT  := turna
VERSION := $(or $(IMAGE_TAG),$(shell git describe --tags --first-parent --match "v*" 2> /dev/null || echo v0.0.0))

.DEFAULT_GOAL := help

.PHONY: test coverage help html html-gen html-wsl run vault consul

run: CONFIG_FILE ?= _example/config/local.yml
run: ## Run the application; CONFIG_FILE to specify a config file
	CONFIG_FILE=$(CONFIG_FILE) go run cmd/$(PROJECT)/main.go

# go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o $(PROJECT) cmd/$(PROJECT)/main.go
build: ## Build the binary
	goreleaser build --snapshot --rm-dist --single-target

vault: ## Run vault server
	docker run --rm -it -p 8200:8200 --name vault vault:latest

consul: ## Run consul server
	docker run --rm -it -p 8500:8500 --name consul consul:latest

test: ## Run unit tests
	@go test -race -cover ./...

coverage: ## Run unit tests with coverage
	@go test -v -race -cover -coverpkg=./... -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out

html: ## Show html coverage result
	@go tool cover -html=./coverage.out

html-gen: ## Export html coverage result
	@go tool cover -html=./coverage.out -o ./coverage.html

html-wsl: html-gen ## Open html coverage result in wsl
	@explorer.exe `wslpath -w ./coverage.html` || true

help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
