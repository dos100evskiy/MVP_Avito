# Авито.Кухня - MVP

[![CI](https://github.com/dos100evskiy/MVP_Avito/actions/workflows/ci.yml/badge.svg)](https://github.com/talense-tasks/backend-trainee-assignment-autumn-2026-dos100evskiy-6b58b7b5/actions/workflows/ci.yml)

Backend-сервис агрегатора доставки еды заведений.
Тестовое задание, backend trainee, осень 2026.

## Состав репозитория

```
avito-kuhnya/
├── core/                 # основной сервис: Client API + Venue API
├── restaurant-service/   # пример заведения (демонстрация интеграции)
├── api/openapi/          # OpenAPI-спецификации обоих API
├── docs/
│   ├── diagrams/         # PlantUML: CJM клиента/заведения, C4 context/container
│   ├── db-schema.md
│   └── ai-usage/prompts.md
├── docker-compose.yml
├── Makefile
└── .golangci.yml
```

## Быстрый старт

```bash
cp core/.env.example core/.env   # при необходимости поменять порт/DSN
make up                          # docker compose up --build
```

После старта:
- Core API: `http://localhost:8080` (health: `/health`)
- Restaurant Service: `http://localhost:8090` (health: `/health`)
- Postgres: `localhost:5432` (`avito`/`avito`/`avito_kuhnya`)

В dev-миграции (`core/migrations/000002_seed_dev_data.up.sql`) уже создано
тестовое заведение с API-ключом `dev-venue-key` и несколько позиций меню -
им и пользуется `restaurant-service` из коробки.

### Пример сценария вручную

```bash
# Список заведений
curl http://localhost:8080/api/v1/client/venues

# Меню заведения (id из ответа выше)
curl http://localhost:8080/api/v1/client/venues/<venueId>/menu

# Оформить заказ
curl -X POST http://localhost:8080/api/v1/client/orders \
  -H 'Content-Type: application/json' \
  -d '{"venueId":"<venueId>","customerRef":"demo-user","items":[{"menuItemId":"<itemId>","quantity":2}]}'

# Статус заказа (restaurant-service сам примет и проведёт заказ по статусам)
curl http://localhost:8080/api/v1/client/orders/<orderId>
```

## Архитектура

C4-диаграммы (context/container) - `docs/diagrams/c4-context.puml`,
`docs/diagrams/c4-container.puml`. Сгенерировать SVG: `make diagrams`
(требует Docker, использует образ `plantuml/plantuml`).

Кратко: `core` - монолит
(`domain` -> `usecase` -> `transport/http` -> `repository/postgres`), Client API
и Venue API как отдельные группы роутов с разной моделью авторизации.
`restaurant-service` - независимый Go-модуль, имитирующий backend реального
заведения: получает заказы через webhook (push) с fallback на поллинг Venue
API, эмулирует прохождение заказа по статусам.

Подробный план и обоснования решений - `avito-kuhnya-plan.md`

## Кодогенерация из OpenAPI

Типы запросов/ответов (`Venue`, `Order`, `CreateOrderRequest` и т.д.) не
пишутся руками — генерируются из `api/openapi/*.yaml` через
[`oapi-codegen`]:

```bash
make generate
```

Генерируются только модели (`generate: models`), без переписывания роутинга —
сам HTTP-роутинг (`chi`) и хендлеры остаются написанными вручную, но
структуры для JSON-декодирования/энкодинга теперь одна на двоих со спекой:
`core/internal/transport/http/client/types.gen.go` и
`.../venue/types.gen.go`. Файлы с суффиксом `.gen.go` не редактируются
руками (шапка `DO NOT EDIT`) — если нужно изменить форму
запроса/ответа, меняется `api/openapi/*.yaml`, дальше `make generate`.

Технические детали, если будете разбираться в коде:
- Поля, необязательные по спеке (без `required`), становятся указателями
  (`*string`, `*int64`...) — для них есть небольшой generic-хелпер
  `httpapi.Ptr[T](v T) *T` в `internal/transport/http/ptr.go`, чтобы не
  заводить временную переменную под каждое поле при сборке ответа.
- Поля с `format: uuid` в спеке помечены расширением `x-go-type: string`,
  чтобы генератор не подставлял `uuid.UUID` вместо привычного `string`,
  которым everywhere оперирует `domain`-слой — иначе пришлось бы парсить
  UUID на каждой границе ради нулевой практической пользы в этом MVP.
- `client.Order` и `venue.Order` — теперь два разных типа (раньше был один
  общий `OrderDTO` на оба API). Это вскрыло реальное расхождение: ответ
  `GET /venue/orders` раньше отдавал лишнее `"items":null`, которого в
  OpenAPI-спеке Venue API никогда не было — молчаливый дрейф спеки и кода,
  который кодогенерация физически не позволяет повторить.

## CJM

`docs/diagrams/cjm_client.puml` и `docs/diagrams/cjm_venue.puml` - сценарии
клиента и заведения.

## Схема БД

См. `docs/db-schema.md` и `core/migrations/000001_init.up.sql`.

## Линтер

```bash
make lint
```

Конфигурация - `.golangci.yml` (govet, staticcheck, errcheck, gosec, revive,
bodyclose, unconvert, gofmt/goimports).

## Тесты

```bash
make test
make test-race
make test-docker
```

Реализованы юнит-тесты usecase-слоя на ручных моках репозиториев (без
mockgen/testify - минимум зависимостей для MVP):
- `internal/domain/models_test.go` - конечный автомат статусов заказа
  (`CanTransition`), 80% покрытия пакета `domain`.
- `internal/usecase/order_test.go` - создание заказа (успех, недоступная
  позиция, позиции из разных заведений, пустой заказ), переходы статусов
  (валидный/невалидный), отмена заказа, а также регрессионный тест
  `TestCreateOrder_HistoryWrittenInSameTx` на баг с записью истории статуса
  вне транзакции создания заказа (FK-нарушение на реальном Postgres).
- `internal/usecase/catalog_test.go` - получение меню заведения.

67% покрытия пакета `usecase`, 80% - `domain`.

Для `restaurant-service` тоже есть тесты (реальные HTTP-запросы через
`httptest.Server`, без моков-интерфейсов - `coreclient.Client` и
`webhook.NewHandler` принимают конкретные значения, поэтому достаточно
поднять тестовый HTTP-сервер и направить клиента на него):
- `internal/coreclient/client_test.go` - все методы клиента к Venue API
  (accept/reject/status/list), включая проверку заголовков и тела запроса
  (81% покрытия).
- `internal/kitchen/emulator_test.go` - полный жизненный цикл заказа
  (`accept → COOKING → READY → DELIVERING → COMPLETED`), обрыв пайплайна
  при ошибке на этапе accept, работа поллера (80% покрытия). Реальные
  задержки 2-5с на шаг подменяются на мгновенные через package-level
  переменные `sleepFunc`/`delayFunc`.
- `internal/webhook/handler_test.go` - приём валидного/невалидного вебхука
  (100% покрытия).

- `make test` - прогон **без** `-race` (используется по умолчанию, работает
  везде без дополнительной настройки);
- `make test-race` - прогон **с** `-race`;
- `make test-docker` - race-тесты внутри линуксового `golang`-контейнера,
  самый надёжный вариант.

### HTTP-тесты хендлеров core

`internal/transport/http/client/handlers_test.go` и
`.../venue/handlers_test.go` - через `httptest`, без реальной БД. Чтобы это
стало возможным, `client.Handlers`/`venue.Handlers` принимают маленькие 
интерфейсы (`CatalogUseCase`,`OrderUseCase`, `MenuUseCase` - объявлены в 
самих пакетах транспортного слоя), которые в тестах подменяются лёгкими моками.

Проверяется полный HTTP-путь: маршрутизация `chi`, декодирование
JSON-запроса в сгенерированные из OpenAPI типы, маппинг domain-ошибок в
HTTP-статусы (`apierror.go`), кодирование ответа обратно в JSON. Для
`venue`-пакета тесты дополнительно проходят через настоящий
`middleware.VenueAuth` (с моком `domain.VenueRepository`) - так же, как в
проде, а не в обход авторизации.

Покрытие: `client` ~95%, `venue` ~97%.

### Интеграционные тесты репозиториев

```bash
make test-integration
```

Поднимают настоящий PostgreSQL 16 в Docker через `testcontainers-go`,
накатывают миграции из `core/migrations/*.up.sql` и гоняют реальные SQL-запросы
репозиториев - `core/internal/repository/postgres/*_integration_test.go`.
Помечены билд-тегом `integration`, поэтому **не запускаются** обычным
`make test` (не требуют Docker для повседневной разработки) 76% покрытия.

## CI

`.github/workflows/ci.yml` — запускается на каждый push и на
каждый pull request. Шесть job'ов, каждый со своей задачей:

| Job | Что делает | Зачем отдельно |
|---|---|---|
| `lint` | `golangci-lint` для `core` и `restaurant-service` | падает за секунды, не требует сети/Docker |
| `generate-check` | `make generate`, затем `git diff` по `*.gen.go` | ловит рассинхронизацию OpenAPI-спеки и сгенерированного кода — ровно та проблема, которую кодогенерация должна устранять (см. план, раздел 6.4) |
| `unit-test` | `go test ./... -race -cover` для обоих модулей | без Docker, без testcontainers |
| `integration-test` | `go test -tags=integration ./internal/repository/postgres/...` | PostgreSQL через `testcontainers-go`; `ubuntu-latest` |
| `build` | `go build ./...` для обоих модулей | дешёвая защита от "забыл закоммитить файл" |
| `docker-smoke` | `docker compose build` + `up --wait` + E2E-сценарий через `curl`/`jq` (список заведений → меню → заказ → получение заказа) | напрямую проверяет критерий приёмки №1 из ТЗ ("приложение поднимается и выполняет бизнес-сценарии"); заодно ловит рассинхрон версии Go между `go.mod` и Docker-образами — баг, который уже один раз чинилcz вручную (план, раздел 11.3 п.5) |

## Допущения и упрощения (MVP)

- Авторизация/аутентификация пользователей не реализована (по условию задания) - customerRef в 
заказе является произвольной строкой-идентификатором клиента (email, телефон или любой другой опознаватель со стороны фронтенда); 
связь "заказ - пользователь" в БД строится через это поле, без отдельной таблицы пользователей и без FK - сознательное упрощение MVP;
- Заведения авторизуются статическим API-ключом (`X-API-Key`), без выдачи/
  ротации ключей и полноценного OAuth;
- Один заказ = позиции только одного заведения;
- Логистика/курьер не моделируется: переход `DELIVERING → COMPLETED`
  выполняет сам `restaurant-service`;
- Оплата вне скоупа: `totalKopecks` считается, но не проводится через
  платёжный шлюз;
- Конкурентный доступ к остаткам решён через `SELECT ... FOR UPDATE` в
  транзакции создания заказа - без полноценной резервации/склада.
- Доставка уведомления о новом заказе заведению — HTTP webhook (best-effort, без ретраев) + 
 fallback-поллинг GET /venue/orders?status=CREATED на случай, если webhook не долетел.
 