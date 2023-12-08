PROJECT  := turna
VERSION := $(or $(IMAGE_TAG),$(shell git describe --tags --first-parent --match "v*" 2> /dev/null || echo v0.0.0))

.DEFAULT_GOAL := help

.PHONY: run
run: export LOG_LEVEL ?= debug
run: export CONFIG_FILE ?= testdata/config/local.yml
run: ## Run the application; CONFIG_FILE to specify a config file
	go run cmd/$(PROJECT)/main.go

# go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o $(PROJECT) cmd/$(PROJECT)/main.go
.PHONY: build
build: ## Build the binary
	goreleaser build --snapshot --clean --single-target

.PHONY: lint
lint: ## Run linter
	GOPATH="$(shell dirname $(PWD))" golangci-lint run

.PHONY: lint-act
lint-act: ## Run linter in act
	act -W .github/workflows/test.yml -j sonarcloud

.PHONY: vault
vault: ## Run vault server
	docker run --rm -it -p 8200:8200 --name vault vault:latest

.PHONY: consul
consul: ## Run consul server
	docker run --rm -it -p 8500:8500 --name consul consul:latest

.PHONY: keycloak
keycloak: ## Run keycloak server
	docker run --rm -it -p 8080:8080 -e KEYCLOAK_ADMIN=admin -e KEYCLOAK_ADMIN_PASSWORD=admin quay.io/keycloak/keycloak:20.0.2 start-dev

.PHONY: whoami
whoami: ## Run whoami server
	docker run --rm -it -p 9090:80 traefik/whoami:latest

.PHONY: test
test: ## Run unit tests
	@go test  -timeout 30s -race -cover ./...

.PHONY: coverage
coverage: ## Run unit tests with coverage
	@go test -v -race -cover -coverpkg=./... -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out

.PHONY: html
html: ## Show html coverage result
	@go tool cover -html=./coverage.out

.PHONY: html-gen
html-gen: ## Export html coverage result
	@go tool cover -html=./coverage.out -o ./coverage.html

.PHONY: html-wsl
html-wsl: html-gen ## Open html coverage result in wsl
	@explorer.exe `wslpath -w ./coverage.html` || true

.PHONY: help
help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
