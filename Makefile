.DEFAULT_GOAL := help

GO      ?= go
PKG     := ./...
COVER   := coverage.out

# Ports are offset so they do not collide with a database already running
# locally on the default port.
PG_PORT      ?= 55432
MYSQL_PORT   ?= 53306
MARIADB_PORT ?= 53307

export RUNG_TEST_POSTGRES_DSN = postgres://rung:rung@127.0.0.1:$(PG_PORT)/rung?sslmode=disable
export RUNG_TEST_MYSQL_DSN    = rung:rung@tcp(127.0.0.1:$(MYSQL_PORT))/rung
export RUNG_TEST_MARIADB_DSN  = rung:rung@tcp(127.0.0.1:$(MARIADB_PORT))/rung

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build every package and the CLI
	$(GO) build $(PKG)

.PHONY: fmt
fmt: ## Format the source
	gofmt -w .

.PHONY: vet
vet: ## Run go vet
	$(GO) vet $(PKG)

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: test
test: ## Run the unit tests (integration tests skip without databases)
	$(GO) test -race $(PKG)

.PHONY: test-integration
test-integration: ## Run every test, including the ones needing databases
	$(GO) test -race -covermode=atomic -coverprofile=$(COVER) $(PKG)
	$(GO) tool cover -func=$(COVER) | tail -n 1

.PHONY: cover
cover: test-integration ## Open the coverage report in a browser
	$(GO) tool cover -html=$(COVER)

.PHONY: check
check: fmt vet lint test ## Everything CI runs, short of the databases

.PHONY: db-up
db-up: ## Start PostgreSQL, MySQL and MariaDB in Docker
	docker run -d --name rung-pg \
		-e POSTGRES_USER=rung -e POSTGRES_PASSWORD=rung -e POSTGRES_DB=rung \
		-p $(PG_PORT):5432 postgres:16-alpine
	docker run -d --name rung-mysql \
		-e MYSQL_ROOT_PASSWORD=rung -e MYSQL_DATABASE=rung \
		-e MYSQL_USER=rung -e MYSQL_PASSWORD=rung \
		-p $(MYSQL_PORT):3306 mysql:8.4
	docker run -d --name rung-mariadb \
		-e MARIADB_ROOT_PASSWORD=rung -e MARIADB_DATABASE=rung \
		-e MARIADB_USER=rung -e MARIADB_PASSWORD=rung \
		-p $(MARIADB_PORT):3306 mariadb:11.4
	@echo "Waiting for the databases to accept connections..."
	@sleep 40

.PHONY: db-down
db-down: ## Remove the test databases
	-docker rm -f rung-pg rung-mysql rung-mariadb

.PHONY: snapshot
snapshot: ## Build the release artifacts without publishing
	goreleaser release --snapshot --clean --skip=publish

.PHONY: clean
clean: ## Remove build output
	rm -rf dist $(COVER)
