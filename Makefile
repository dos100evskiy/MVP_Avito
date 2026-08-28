.PHONY: up down logs migrate-up migrate-down lint test test-race test-docker test-integration generate build diagrams

up:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f core restaurant-service

migrate-up:
	docker compose run --rm core-migrate

migrate-down:
	docker compose run --rm core-migrate \
		-path /migrations \
		-database "postgres://avito:avito@postgres-core:5432/avito_kuhnya?sslmode=disable" \
		down 1

lint:
	cd core && golangci-lint run ./...
	cd restaurant-service && golangci-lint run ./...

# Обычный прогон тестов, без -race. Работает на любой ОС без CGO/gcc-тулчейна —
# используйте это по умолчанию, особенно на Windows.
test:
	cd core && go test ./... -cover
	cd restaurant-service && go test ./... -cover

# Race detector
test-race:
	cd core && go test ./... -race -cover
	cd restaurant-service && go test ./... -race -cover

# Race-тесты в Docker
test-docker:
	docker run --rm -e GOFLAGS=-mod=mod -v $(CURDIR)/core:/app -w /app golang:1.25 go test ./... -race -cover
	docker run --rm -e GOFLAGS=-mod=mod -v $(CURDIR)/restaurant-service:/app -w /app golang:1.25 go test ./... -race -cover

# Интеграционные тесты репозиториев: поднимают настоящий PostgreSQL в Docker
# через testcontainers-go (пакет internal/repository/postgres/*_integration_test.go,
# build tag "integration" — поэтому НЕ запускаются обычным `test`).
# Требуют Docker-демон, доступный тому процессу, что запускает go test —
# на Windows/macOS это Docker Desktop, должен быть запущен заранее.
test-integration:
	cd core && go test -tags=integration ./internal/repository/postgres/... -v -cover

# Перегенерировать Go-типы запросов/ответов из OpenAPI-спек (api/openapi/*.yaml)
# через oapi-codegen. Запускать после любого изменения спеки — типы в
# core/internal/transport/http/{client,venue}/types.gen.go должны оставаться
# синхронными со спекой (это и есть весь смысл кодогенерации: спека —
# источник правды, а не что-то, что нужно вручную дублировать в Go-структурах).
# Требует доступ в интернет (go run скачивает oapi-codegen с proxy.golang.org
# при первом запуске) — на CI/локально с обычным доступом работает "из коробки".
generate:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 \
		-config core/internal/transport/http/client/oapi-codegen-config.yaml \
		api/openapi/core-client-api.yaml
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 \
		-config core/internal/transport/http/venue/oapi-codegen-config.yaml \
		api/openapi/core-venue-api.yaml

build:
	cd core && go build ./...
	cd restaurant-service && go build ./...

# Требует Docker (использует образ plantuml/plantuml) для рендера .puml -> .svg
diagrams:
	docker run --rm -v $(CURDIR)/docs/diagrams:/data plantuml/plantuml -tsvg /data/*.puml
