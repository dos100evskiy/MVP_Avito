# План реализации сервиса «Авито.Кухня» (backend trainee, осень 2026)

> **Статус: реализовано, документ обновлён по факту.** Изначально это был
> план-до-кода (contract-first). Сейчас он служит и планом, и отчётом о
> реализации - расхождения с фактическим кодом отмечены пометками
> **[РЕАЛИЗОВАНО]** / **[ИЗМЕНЕНО]** по ходу текста, а в разделе 11 собран
> итог: что сделано, что найдено и исправлено в процессе, что осталось.

## 0. Постановка задачи

Нужно спроектировать и реализовать MVP платформы доставки еды:

- **Core-сервис (Авито.Кухня)** - API для клиентской части (просмотр заведений, меню, оформление заказа) и API для заведений (управление меню, приём и статус заказов).
- **Restaurant-сервис** - отдельный микросервис "заведения", имитирующий реальную точку общепита, который забирает заказы у core-сервиса и меняет их статус (пример интеграции).
- Всё поднимается через `docker-compose up`, БД - PostgreSQL, миграции, OpenAPI-спека, PlantUML CJM, линтер, C4-диаграммы в README.

Ниже - пошаговый план, который затем превращается в реализацию.

---

## 1. Пользовательские сценарии (CJM) - сначала бизнес, потом код

**[РЕАЛИЗОВАНО]** Оба CJM реализованы как описано ниже, без отклонений от
плана. Диаграммы лежат в `docs/diagrams/*.puml`, сгенерированы в `.svg`
через `make diagrams`.

### 1.1 CJM клиента

1. Клиент открывает Авито.Кухня -> видит список доступных заведений (`GET /catalog/venues`), с фильтрами (город, категория, "открыто сейчас").
2. Клиент выбирает заведение -> видит меню (`GET /catalog/venues/{id}/menu`) с категориями блюд, ценами, признаком "в наличии".
3. Клиент формирует корзину на фронте (без бэкенд-стейта корзины, либо с легковесной сессией - решить и обосновать в README).
4. Клиент пытается добавить блюдо, которого нет в наличии -> фронт получает 409/сервис отдаёт актуальный `is_available=false`, сценарий "товара нет в наличии" отрабатывается на этапе оформления заказа повторной проверкой остатков.
5. Клиент оформляет заказ (`POST /orders`) -> сервис:
   - валидирует, что все позиции принадлежат одному заведению (доставка/приготовление из одного места в MVP - типовое упрощение, описать в README);
   - валидирует наличие и актуальную цену каждой позиции (защита от гонки/устаревшей цены на фронте);
   - создаёт заказ в статусе `CREATED`, резервирует позиции.
6. Если часть позиций закончилась между просмотром меню и оформлением заказа -> сервис возвращает 409 с деталями, какие позиции недоступны (сценарий "конфликт наличия").
7. Клиент отслеживает статус заказа (`GET /orders/{id}`), получает смену статусов: `CREATED -> ACCEPTED -> COOKING -> READY -> DELIVERING -> COMPLETED`, либо `CANCELLED`.
8. Клиент может отменить заказ, пока он в статусе `CREATED`/`ACCEPTED` (`POST /orders/{id}/cancel`).

### 1.2 CJM заведения

1. Заведение аутентифицируется по API-ключу (упрощённая авторизация, т.к. по ТЗ полноценный auth не требуется - используем статический API-key/venue_id в заголовке, обосновать в README).
2. Заведение управляет своим меню:
   - создаёт/обновляет позиции (`POST/PATCH /venue-api/menu-items`);
   - помечает позиции как временно недоступные (`PATCH /venue-api/menu-items/{id}/availability`).
3. Заведение получает новые заказы:
   - синхронно через `GET /venue-api/orders?status=CREATED`, и/или
   - асинхронно - core-сервис нотифицирует restaurant-сервис через вебхук `POST {venue.callback_url}/orders` (показываем оба варианта интеграции - polling и push, это и есть "пример интеграции" из ТЗ).
4. Заведение принимает заказ (`POST /venue-api/orders/{id}/accept`) -> статус `ACCEPTED`.
5. Заведение готовит и обновляет статус (`PATCH /venue-api/orders/{id}/status` -> `COOKING -> READY`).
6. Заведение (или курьерский модуль-заглушка) переводит в `DELIVERING -> COMPLETED`.
7. Заведение может отклонить заказ (`POST /venue-api/orders/{id}/reject`) с причиной -> статус `CANCELLED`, клиенту показывается причина.

Эти два CJM оформляются как **PlantUML activity/sequence diagrams**, генерируются в CI или скриптом (`make diagrams`) в `docs/diagrams/*.puml` -> `.svg`.

> Примечание: пути в описании CJM выше (`/catalog/venues`, `/venue-api/...`)
> иллюстративные - реальные префиксы, зафиксированные в OpenAPI и коде,
> это `/api/v1/client/...` и `/api/v1/venue/...` (см. раздел 6).

Пример заготовки (`docs/diagrams/cjm_client.puml`):

```plantuml
@startuml
title CJM: Клиент оформляет заказ
start
:Открыть список заведений;
:Выбрать заведение;
:Просмотреть меню;
if (Позиция в наличии?) then (да)
  :Добавить в корзину;
else (нет)
  :Показать "недоступно";
endif
:Оформить заказ (POST /orders);
if (Все позиции валидны?) then (да)
  :Заказ создан (CREATED);
  :Ожидание подтверждения заведения;
else (нет)
  :409 Conflict + список недоступных позиций;
  stop
endif
:Отслеживание статуса заказа;
stop
@enduml
```

---

## 2. Архитектура (C4)

**[РЕАЛИЗОВАНО]** Все три уровня диаграмм есть в `docs/diagrams/` (`.puml` +
сгенерированные `.svg`). Компонентная структура core-сервиса ниже -
**[ИЗМЕНЕНО]** в одном месте относительно исходного плана: роутер вынесен в
отдельный подпакет `transport/http/server`, а не лежит прямо в
`transport/http` вместе с DTO/error-хендлингом. Причина - реальный цикл
импорта, найденный при написании кода: `transport/http` содержит
`WriteError` и общий `Ptr`-хелпер, которые нужны хендлерам в `client`/`venue`;
а роутер (который должен импортировать `client`/`venue`, чтобы их
смонтировать) не может лежать в том же пакете - иначе получается
`transport/http` -> `client` -> `transport/http`. Решение: `server` подпакет
знает про `client`, `venue` и `httpapi`, а `client`/`venue` знают только про
`httpapi`, не про `server`.

### 2.1 C4 Level 1 - Context

Действующие лица и системы:
- **Пользователь (веб-клиент Авито.Кухня)** - не реализуется, но потребляет Client API.
- **Заведение** - использует Venue API core-сервиса ИЛИ имеет свой backend (restaurant-service), который сам ходит в core через Venue API.
- **Авито.Кухня (core service)** - центральная система.
- **Restaurant service (пример заведения)** - отдельный процесс/контейнер, демонстрирующий интеграцию.
- **PostgreSQL** - хранилище core-сервиса.
- (Опционально) **PostgreSQL/SQLite restaurant-сервиса** - своя маленькая БД для имитации "внутренней кухни".

### 2.2 C4 Level 2 - Container diagram

```
+-------------------+        HTTPS/REST        +---------------------------+
|  Web Client (вне   | ------------------------> |   Avito.Kuhnya Core API   |
|  скоупа реализации)|                            |  (Go, net/http+chi, REST) |
+-------------------+                             +-------------+-------------+
                                                          |          ^
                                       Client API/Venue API|          | Webhook (push new orders)
                                                          v          |
                                                  +---------------------------+
                                                  |     PostgreSQL (core)     |
                                                  +---------------------------+
                                                          ^
                                                          | Venue API (REST)
                                                          |
                                                  +---------------------------+
                                                  |  Restaurant Service       |
                                                  |  (Go, пример заведения)   |
                                                  +-------------+-------------+
                                                                |
                                                                v
                                                  +---------------------------+
                                                  |  PostgreSQL/SQLite (venue)|
                                                  +---------------------------+
```

### 2.3 C4 Level 3 - Component diagram core-сервиса

Архитектура, разбитая по доменам. Ниже - **фактическая**
структура (см. пояснение о `server` выше):

```
cmd/
  core/               # main() core-сервиса
internal/
  config/             # загрузка конфигурации из env
  domain/             # сущности, ошибки, интерфейсы репозиториев (Venue, MenuItem, Order, OrderItem)
  usecase/            # бизнес-логика: catalog, order, venue-menu
  transport/
    http/
      apierror.go        # единый формат ошибок, маппинг domain-ошибок в HTTP-статусы
      ptr.go              # generic-хелпер Ptr[T] для указателей в сгенерированных типах
      server/             # сборка роутера (chi), монтирование client/venue
      client/             # хендлеры Client API + types.gen.go (сгенерированные из OpenAPI DTO)
      venue/               # хендлеры Venue API + types.gen.go (сгенерированные из OpenAPI DTO)
      middleware/           # логирование, recover, request-id, api-key auth
  repository/
    postgres/          # реализация репозиториев через pgx (venue/menu/order + tx-менеджер)
  infrastructure/
    webhook/            # push-нотификатор заведений о новом заказе
  logger/             # инициализация slog
migrations/           # sql-миграции (golang-migrate)
api/
  openapi/
    core-client-api.yaml   # источник правды для client/types.gen.go
    core-venue-api.yaml     # источник правды для venue/types.gen.go
```

**[ИЗМЕНЕНО]** относительно исходного плана:
- нет `pkg/apierror` - маппинг ошибок живёт прямо в `transport/http/apierror.go`, отдельный публичный пакет не понадобился;
- `config`/`logger` - не под `infrastructure/`, а отдельными пакетами верхнего уровня `internal/config`, `internal/logger` (симметрично остальным `internal/*`);
- **[ЧАСТИЧНО РЕАЛИЗОВАНО]** DTO не пишутся вручную - `client/types.gen.go` и `venue/types.gen.go`, сгенерированные из OpenAPI (см. раздел 6.4 и раздел 11). `sqlc` для репозиториев не подключался - только `oapi-codegen` для HTTP-слоя.

Компоненты общаются строго сверху вниз (handler -> usecase -> repository), никаких прямых SQL-запросов из хендлеров - легко тестировать usecase моками репозиториев.

---

## 3. Технологический стек

**[ИЗМЕНЕНО]** Ниже - таблица с фактическим выбором вместо изначального
плана. Главное отклонение осталось одно: `sqlc` для SQL-запросов
репозиториев не подключался (упрощение MVP). А вот **`oapi-codegen` - в
отличие от более раннего состояния проекта, теперь реально подключён и
используется**: Go-типы запросов/ответов HTTP-слоя генерируются из OpenAPI,
а не пишутся вручную. Также вместо `testify` в тестах - ручные моки на
стандартном `testing` (меньше зависимостей), `testcontainers-go` реально
используется для интеграционных тестов репозиториев.

| Категория | Выбор | Обоснование |
|---|---|---|
| Язык | Go 1.25 (единая версия - `go.mod`, `Dockerfile`, `docker-compose.yml`, локальный тулчейн) | по рекомендации ТЗ; версия синхронизирована по всему проекту |
| HTTP-роутер | `chi` | легковесный, стандартный net/http-совместимый |
| Работа с БД | `pgx/v5` (без `sqlc`) | строгая типизация через pgx; `sqlc` рассматривался, но кодогенерация SQL-слоя не подключена - см. раздел 11 |
| Миграции | `golang-migrate` (образ `migrate/migrate` в docker-compose) | простые up/down sql-файлы |
| Валидация | ручные проверки в хендлерах | см. TODO в разделе 11 |
| Логирование | `log/slog` | не тащит лишних зависимостей |
| Конфигурация | ручной парсинг `os.LookupEnv` в `internal/config` | простота, не тащит лишних зависимостей |
| Линтер | `golangci-lint` | стандарт для Go-проектов, конфиг `.golangci.yml` в репо |
| Юнит-тесты | стандартный `testing` + ручные моки (без `testify`/`mockgen`) | минимум зависимостей для MVP |
| Интеграционные тесты | `testcontainers-go` + `testcontainers-go/modules/postgres`, реальный PostgreSQL 16 в Docker | ловит то, что моки принципиально не видят - FK, блокировки, NULL-сканирование (см. раздел 11) |
| **OpenAPI** | `oapi-codegen` (`generate: models`) - Go-типы запросов/ответов генерируются из `api/openapi/*.yaml`** | Генерируются только модели, не серверные интерфейсы - роутинг (`chi`) и хендлеры остаются ручными; см. раздел 6.4 |
| Контейнеризация | Docker + Docker Compose | по ТЗ |
| CI | GitHub Actions | В качества vcs используется git |

---

## 4. Структура репозитория

**[РЕАЛИЗОВАНО]**, структура ниже актуальна на сегодня (тестовые файлы
`*_test.go` и `*_integration_test.go`, а также `*.gen.go` в
`transport/http/{client,venue}` опущены из дерева для краткости - они лежат
рядом с соответствующими `.go`-файлами реализации, см. раздел 11):

```
avito-kuhnya/
├── core/                       # основной сервис Авито.Кухня
│   ├── cmd/core/main.go
│   ├── internal/...
│   ├── migrations/
│   ├── go.mod
│   └── Dockerfile
├── restaurant-service/          # сервис-пример заведения
│   ├── cmd/restaurant/main.go
│   ├── internal/...
│   ├── go.mod
│   └── Dockerfile
├── api/
│   └── openapi/
│       ├── core-client-api.yaml   # источник правды для client/types.gen.go
│       └── core-venue-api.yaml     # источник правды для venue/types.gen.go
├── docs/
│   ├── diagrams/                # .puml + сгенерированные .svg (make diagrams)
│   │   ├── c4-context.puml / .svg
│   │   ├── c4-container.puml / .svg
│   │   ├── cjm_client.puml / .svg
│   │   └── cjm_venue.puml / .svg
│   ├── db-schema.md              # описание ER-модели
│   └── ai-usage/                 # промпты, использованные для генерации кода
│       └── prompts.md
├── docker-compose.yml
├── Makefile                     # make up / migrate-up / migrate-down / lint / test / test-race / test-docker / test-integration / generate / build / diagrams
├── .golangci.yml
├── .env.example
└── README.md
```

Два независимых Go-модуля (`core`, `restaurant-service`) - реалистично имитирует "два разных сервиса разных владельцев", как в реальности при интеграции с внешним заведением.

---

## 5. Проектирование БД (PostgreSQL)

**[РЕАЛИЗОВАНО]**

### 5.1 Сущности

- **venues** - заведения (id, name, description, address, city, is_active, api_key_hash, callback_url, created_at)
- **menu_categories** - категории меню (id, venue_id, name, sort_order)
- **menu_items** - позиции меню (id, venue_id, category_id, name, description, price, currency, is_available, created_at, updated_at)
- **orders** - заказы (id, venue_id, status, total_price, currency, customer_ref, created_at, updated_at)
- **order_items** - позиции заказа (id, order_id, menu_item_id, name_snapshot, price_snapshot, quantity)
- **order_status_history** - журнал смены статусов (id, order_id, from_status, to_status, changed_at, changed_by) - полезно для аудита и отладки сценариев "заказ отменён/отклонён"

### 5.2 Замечания к дизайну

- `menu_items.price` и `order_items.price_snapshot` - денежные суммы храним в `numeric(10,2)` либо в минимальных единицах (`bigint`, копейки) - предпочтительно последнее, чтобы избежать проблем округления. **[РЕАЛИЗОВАНО как `price_kopecks`/`price_snapshot`, `bigint`.]**
- В `order_items` сохраняем **снимок** цены и названия на момент заказа (`price_snapshot`, `name_snapshot`) - меню может измениться после оформления заказа, а история заказа должна быть неизменной.
- `orders.status` - либо `varchar` + `CHECK` constraint, либо Postgres `ENUM` (`order_status`). Для MVP проще `ENUM`, но `varchar+CHECK` легче мигрировать при добавлении статусов - выбрать и обосновать (я бы взял `varchar + CHECK`, т.к. "решение нужно будет масштабировать" - ALTER TYPE ADD VALUE в PG имеет ограничения внутри транзакций). **[РЕАЛИЗОВАНО: выбран `varchar + CHECK`.]**
- Индексы: `menu_items(venue_id, is_available)`, `orders(venue_id, status)`, `orders(created_at)` для листинга/пагинации.
- Внешние ключи с `ON DELETE RESTRICT` для `venue_id`, чтобы не терять историю заказов при "удалении" заведения (лучше использовать soft-delete `is_active=false`).

> **Найдено интеграционными тестами:** `venues.description` и
> `venues.callback_url` объявлены `NULL`-able (заведение может не указать
> описание), но изначальная реализация `VenueRepo` сканировала `description`
> напрямую в Go `string` без `coalesce(description, '')` - на реальном
> Postgres это падало с `cannot scan NULL into *string`. Юнит-тесты на моках
> такое не ловят, поймал именно интеграционный тест на `testcontainers-go`.
> Исправлено - `coalesce()` теперь используется для обоих nullable-полей.

### 5.3 ER-диаграмма (текстово)

```
venues (1) ──< menu_categories (1) ──< menu_items
venues (1) ──< orders (1) ──< order_items >── menu_items
orders (1) ──< order_status_history
```

### 5.4 Пример миграции (000001_init.up.sql)

```sql
CREATE TABLE venues (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL,
    description   text,
    city          text NOT NULL,
    is_active     boolean NOT NULL DEFAULT true,
    api_key_hash  text NOT NULL,
    callback_url  text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE menu_categories (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id   uuid NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    name       text NOT NULL,
    sort_order int NOT NULL DEFAULT 0
);

CREATE TABLE menu_items (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id     uuid NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    category_id  uuid REFERENCES menu_categories(id) ON DELETE SET NULL,
    name         text NOT NULL,
    description  text,
    price_kopecks bigint NOT NULL CHECK (price_kopecks >= 0),
    currency     char(3) NOT NULL DEFAULT 'RUB',
    is_available boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_menu_items_venue_availability ON menu_items(venue_id, is_available);

CREATE TABLE orders (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id     uuid NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    status       varchar(20) NOT NULL DEFAULT 'CREATED'
        CHECK (status IN ('CREATED','ACCEPTED','COOKING','READY','DELIVERING','COMPLETED','CANCELLED')),
    total_kopecks bigint NOT NULL CHECK (total_kopecks >= 0),
    currency     char(3) NOT NULL DEFAULT 'RUB',
    customer_ref text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_orders_venue_status ON orders(venue_id, status);

CREATE TABLE order_items (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id       uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    menu_item_id   uuid NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    name_snapshot  text NOT NULL,
    price_snapshot bigint NOT NULL,
    quantity       int NOT NULL CHECK (quantity > 0)
);

CREATE TABLE order_status_history (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    from_status varchar(20),
    to_status   varchar(20) NOT NULL,
    changed_at  timestamptz NOT NULL DEFAULT now(),
    changed_by  varchar(20) NOT NULL -- 'CLIENT' | 'VENUE' | 'SYSTEM'
);
```

**[ИЗМЕНЕНО]** В `core/migrations` фактически две миграции, не одна:
`000001_init.up/down.sql` (схема выше) и `000002_seed_dev_data.up/down.sql`
- тестовое заведение с известным API-ключом (`dev-venue-key`,
захешированным через `sha256`) и несколько позиций меню для локальной
разработки/ручных curl-проверок. Только для dev.

---

## 6. API-дизайн (высокоуровнево, детали -> OpenAPI)

**[ИЗМЕНЕНО]** Реализовано уже, чем планировалось изначально - часть
эндпоинтов из плана в код не попала.
Зато сама механика работы со спекой стала сильнее плана - см. 6.4.

### 6.1 Client API (`/api/v1/client/...`) - **[РЕАЛИЗОВАНО полностью]**
- `GET /venues` - список заведений (фильтр по городу, пагинация)
- `GET /venues/{venueId}/menu` - меню заведения с категориями
- `POST /orders` - создать заказ (тело: venueId, items[{menuItemId, quantity}], customerRef)
- `GET /orders/{orderId}` - статус и состав заказа
- `POST /orders/{orderId}/cancel` - отмена, если статус позволяет

### 6.2 Venue API (`/api/v1/venue/...`, авторизация через заголовок `X-API-Key`)
- `POST /menu-items` - **[РЕАЛИЗОВАНО]**
- ~~`POST /menu-categories`, `GET /menu-categories`~~ - **[НЕ РЕАЛИЗОВАНО]**: категории создаются только напрямую в БД/миграцией-сидом; управление категориями через API не потребовалось для демонстрации основных сценариев, оставлено как TODO
- ~~`PATCH /menu-items/{id}`~~ (полное обновление позиции) - **[НЕ РЕАЛИЗОВАНО]**: есть только точечный `PATCH /menu-items/{id}/availability`
- `PATCH /menu-items/{id}/availability` - **[РЕАЛИЗОВАНО]**
- `GET /orders?status=CREATED` - новые заказы (poll-режим) - **[РЕАЛИЗОВАНО]**
- `POST /orders/{id}/accept` - **[РЕАЛИЗОВАНО]**
- `POST /orders/{id}/reject` (body: reason) - **[РЕАЛИЗОВАНО]**, но с оговоркой: `reason` пока не персистится отдельным полем в БД, только декодируется - TODO
- `PATCH /orders/{id}/status` (body: status ∈ {COOKING, READY, DELIVERING, COMPLETED}) - **[РЕАЛИЗОВАНО]**

### 6.3 Integration API restaurant-service (пример) - **[РЕАЛИЗОВАНО]**
- принимает вебхук `POST /internal/orders` от core (новый заказ)
- сам вызывает Venue API core-сервиса для accept/reject/статусов - тем самым демонстрируется двусторонняя интеграция
- **[ИЗМЕНЕНО]**: дополнительно к push-вебхуку реализован fallback-поллинг (`GET /venue/orders?status=CREATED` каждые несколько секунд) на случай, если вебхук не долетел

### 6.4 Кодогенерация из OpenAPI - **[РЕАЛИЗОВАНО]**

Изначально в плане предполагалось, что `oapi-codegen` сгенерирует и
DTO-структуры, и серверные интерфейсы (`ServerInterface`).
Сейчас - только модели (`generate: models` в конфиге `oapi-codegen`).

Как это устроено:

```bash
make generate
```

- Конфиги `oapi-codegen-config.yaml` лежат рядом с хендлерами -
  `core/internal/transport/http/client/` и `.../venue/` - и генерируют
  `types.gen.go` прямо в том же пакете, что и сами хендлеры (без отдельного
  namespace для DTO).
- `client.Order` и `venue.Order` - теперь **два разных типа**, каждый
  сгенерирован из своей спеки. Раньше на ручных DTO использовался один общий
  `OrderDTO` на оба API - из-за этого `GET /venue/orders` фактически отдавал
  лишнее поле `"items":null`, которого в OpenAPI-спеке Venue API никогда не
  было. Это реальный баг, найденный именно благодаря переходу на
  кодогенерацию (см. раздел 11) - молчаливый дрейф ручного DTO от спеки,
  который в принципе не может повториться, когда типы генерируются из той же
  спеки, что описывает контракт.
- В спеки добавлено расширение `x-go-type: string` на все поля с
  `format: uuid` - без этого `oapi-codegen` генерирует строгий `uuid.UUID`
  вместо `string`, а весь `domain`-слой и репозитории оперируют ID как
  обычными строками (так их отдаёт `pgx` при сканировании `uuid`-колонок).
  Без этой правки пришлось бы парсить UUID на каждой границе между HTTP- и
  domain-слоем - ради нулевой практической пользы в MVP, где UUID нигде не
  используется как тип, только как непрозрачный идентификатор.
- Поля, необязательные по спеке (без `required`), генерируются как указатели
  (`*string`, `*int64`...) - под это заведён небольшой generic-хелпер
  `httpapi.Ptr[T](v T) *T` (`internal/transport/http/ptr.go`), чтобы не
  заводить временную переменную под каждое поле при сборке ответа.
- Добавлены `operationId` ко всем операциям обеих спек, а три анонимные
  inline-схемы (`isAvailable`, `reason`, `status` в теле запроса)
  промотированы до именованных (`AvailabilityRequest`,
  `RejectOrderRequest`, `UpdateOrderStatusRequest`) - понадобится, если
  позже решим генерировать ещё и серверные интерфейсы, а не только модели.
- Ручной файл с DTO-структурами (`dto.go`) - удалён целиком, ничего на него
  больше не ссылается.
- `sqlc` для SQL-запросов репозиториев по-прежнему **не подключался** -
  кодогенерация закрыла только HTTP-слой, не слой БД.

---

## 7. Docker Compose

**[РЕАЛИЗОВАНО]**, с двумя практичными изменениями относительно
плана - **[ИЗМЕНЕНО]**: во-первых, миграции гоняются готовым образом
`migrate/migrate:v4.17.1` (одноразовый контейнер с
`service_completed_successfully` как условием для старта `core`), а не
самодельным `entrypoint` поверх Dockerfile core-сервиса.

```yaml
services:
  postgres-core:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: avito_kuhnya
      POSTGRES_USER: avito
      POSTGRES_PASSWORD: avito
    ports: ["5432:5432"]
    volumes: [pgdata_core:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U avito -d avito_kuhnya"]
      interval: 5s
      timeout: 3s
      retries: 10

  core-migrate:
    image: migrate/migrate:v4.17.1
    volumes: ["./core/migrations:/migrations"]
    depends_on:
      postgres-core:
        condition: service_healthy
    command: ["-path", "/migrations", "-database", "postgres://avito:avito@postgres-core:5432/avito_kuhnya?sslmode=disable", "up"]
    restart: "no"

  core:
    build: { context: ./core }
    environment:
      HTTP_PORT: 8080
      DATABASE_URL: postgres://avito:avito@postgres-core:5432/avito_kuhnya?sslmode=disable
    ports: ["8080:8080"]
    depends_on:
      postgres-core: { condition: service_healthy }
      core-migrate: { condition: service_completed_successfully }

  restaurant-service:
    build: { context: ./restaurant-service }
    environment:
      HTTP_PORT: 8090
      CORE_API_URL: http://core:8080/api/v1/venue
      CORE_API_KEY: dev-venue-key
      POLL_ENABLED: "true"
    ports: ["8090:8090"]
    depends_on: [core]

volumes:
  pgdata_core:
```

**[ИЗМЕНЕНО]** Фактические цели `Makefile` (шире исходного плана -
добавились `test-race`/`test-docker`/`test-integration`/`generate` в
процессе):

`make up` / `down` / `logs` / `migrate-up` / `migrate-down` / `lint` /
`test` (юнит-тесты, без `-race`) / `test-race` (с `-race`) /
`test-docker` (race-тесты в линуксовом контейнере - обходит проблемы CGO на
Windows) / `test-integration` (testcontainers, реальный Postgres) /
**`generate`** (`oapi-codegen` - только HTTP-типы, `sqlc` не подключался,
см. раздел 6.4) / `build` / `diagrams`.

---

## 8. Порядок разработки (по шагам, инкрементально)

**[РЕАЛИЗОВАНО]** Шаги 1–4, 6–14 пройдены как запланировано. Шаг 5
(кодогенерация) - **[ИЗМЕНЕНО]**: реализован лишь наполовину и не в
исходном месте последовательности. По факту `oapi-codegen` подключили не
на пятом шаге (до написания хендлеров), а **уже после того, как core был
полностью реализован и покрыт тестами** - то есть contract-first в
исходном плане на практике стал retrofit: спека и ручные DTO некоторое
время жили параллельно, и только отдельным заходом ручные DTO были
вычищены в пользу сгенерированных типов (подробности - раздел 6.4 и раздел 11). 
`sqlc` не подключался вообще.

1. **Инициализация репозитория**: структура папок, `go.mod` для двух модулей, `.golangci.yml`, `docker-compose.yml`-скелет, `README.md`-заготовка.
2. **CJM и C4-диаграммы** - сначала product-мышление, фиксируем сценарии в PlantUML (это же служит чек-листом для API).
3. **Проектирование БД**: пишем DDL, миграции, ER-описание в `docs/db-schema.md`.
4. **OpenAPI-спека**: описываем оба API (client/venue) целиком до кода - контракт-first подход.
5. ~~**Кодогенерация**: `sqlc` из SQL-запросов, `oapi-codegen` из OpenAPI.~~ - на этом шаге не сделано; `oapi-codegen` подключён позже, ретроспективно (см. выше), `sqlc` не подключён вообще.
6. **Domain + usecase слой core-сервиса**: бизнес-правила (валидация наличия, снапшоты цен, машина состояний заказа).
7. **HTTP-хендлеры** - **[ИЗМЕНЕНО]**: изначально написаны поверх ручных DTO (кодогенерация ещё не была подключена), затем переписаны на сгенерированные из OpenAPI типы отдельным рефакторингом; middleware (логирование, recover, api-key auth для venue-роутов, request-id) реализованы как в плане, без изменений.
8. **Репозитории** на pgx (без sqlc), транзакционность создания заказа (создание order + order_items в одной транзакции с проверкой остатков через `SELECT ... FOR UPDATE`).
9. **Restaurant-service**: минимальный сервис на Go, который: (а) принимает вебхуки от core, (б) периодически поллит `GET /venue/orders?status=CREATED`, (в) эмулирует бизнес-процесс кухни (авто-переход статусов с задержками через таймеры/горутины для демонстрации).
10. **Docker Compose интеграция**, прогон end-to-end сценария вручную через `curl` (реальные проверки описаны в разделе 11).
11. **Тесты**: unit на usecase (ручные моки на стандартном `testing`, без `mockgen`/`counterfeiter`), интеграционные на репозитории через `testcontainers-go` + реальный PostgreSQL - **[РЕАЛИЗОВАНО шире плана]**, см. раздел 11.
12. **Линтер и статический анализ**: `golangci-lint run ./...` через `make lint`.
13. **README**: описание проекта, архитектура (C4), схема БД, инструкция запуска, допущения и упрощения, промпты ИИ.
14. **Финальная вычитка**: пройтись по критериям приёмки как по чек-листу (раздел 9).
15. **[сверх исходного плана]** **Ретрофит кодогенерации**: подключение `oapi-codegen`, промотирование inline-схем OpenAPI до именованных, добавление `operationId`, `x-go-type: string` для UUID-полей, удаление ручных DTO, обнаружение и фикс расхождения спеки и кода (раздел 6.4, раздел 11).

---

## 9. Чек-лист соответствия критериям приёмки

- [x] `docker-compose up` поднимает core + restaurant-service + Postgres, миграции применяются автоматически (`core-migrate` через `service_completed_successfully`).
- [x] Все основные сценарии (создание заказа, недоступность позиции, отмена, флоу заведения) реализованы и проверены вручную через `curl` + автоматизированными тестами.
- [x] CJM пользователя и заведения - PlantUML-диаграммы в `docs/diagrams`, сгенерированные в SVG (`make diagrams`).
- [x] Миграции с полной схемой БД - в `core/migrations` (плюс dev-сид).
- [x] OpenAPI-спека - в `api/openapi`, покрывает оба API, **и служит источником правды для Go-кода** через `make generate` (см. раздел 6.4) - сильнее исходного требования "просто наличие спеки".
- [x] README: описание проекта / C4 (L2+L3) / схема БД / запуск / допущения.
- [x] Линтер настроен (`.golangci.yml`), проходит без ошибок (`make lint`).
- [x] Промпты ИИ задокументированы в `docs/ai-usage/prompts.md`.
- [x] **[сверх исходного плана]** Юнит-тесты `domain`/`usecase` (моки) - 80%/67% покрытия.
- [x] **[сверх исходного плана]** Юнит-тесты `restaurant-service` (`coreclient`/`kitchen`/`webhook` через `httptest`) - 81%/80%/100% покрытия.
- [x] **[сверх исходного плана]** Интеграционные тесты `repository/postgres` на реальном PostgreSQL через `testcontainers-go` - поймали 2 реальных бага (см. раздел 11).
- [x] **[сверх исходного плана]** Кодогенерация Go-типов из OpenAPI (`oapi-codegen`) - поймала ещё один реальный баг расхождения спеки и кода (раздел 6.4, раздел 11).
- [x] **[сверх исходного плана]** HTTP-тесты хендлеров core (`httptest` + моки usecase) — `client` ~95%, `venue` ~97% (см. раздел 11.2).
- [x] **[сверх исходного плана]** CI (GitHub Actions: lint + generate-check + unit/integration test + build + docker-compose smoke-test) — 6 job'ов, см. раздел 11.5.

---

## 10. Ключевые допущения для README

**[РЕАЛИЗОВАНО]** - весь список ниже вошёл в `README.md`:

- Авторизация и аутентификация пользователей не реализуется (по ТЗ) - `customerRef` в заказе является произвольной строкой-идентификатором клиента; связь "заказ ↔ пользователь" в БД строится через это поле, без отдельной таблицы пользователей и без FK - сознательное упрощение MVP.
- Авторизация заведений упрощена до статического API-key per venue (без выдачи/ротации ключей, без OAuth) - достаточно для демонстрации разделения доступа. Ключ хранится в БД как `sha256`-хеш, не в открытом виде.
- Один заказ = позиции только одного заведения (типично для агрегаторов на MVP-этапе; мультизаведенческая корзина - TODO для след. итерации).
- Курьерская логистика не моделируется - статус `DELIVERING -> COMPLETED` переключается либо вручную через Venue API, либо автоматически таймером в restaurant-service (эмуляция).
- Оплата не входит в скоуп - `totalKopecks` рассчитывается, но процессинг не реализуется.
- Конкурентный доступ к остаткам (`is_available`) решается через `SELECT ... FOR UPDATE` на затрагиваемые `menu_items` в транзакции создания заказа, без полноценной резервации/склада.
- Доставка уведомления о новом заказе заведению - HTTP webhook (best-effort) + fallback-поллинг. Это сознательный выбор в пользу простоты демо: при реальном масштабировании корректный паттерн - не заменить HTTP на прямую интеграцию через брокер (Kafka и т.п.), а держать брокер/outbox **внутри** core, тогда как контракт **наружу**, к заведению, остаётся HTTP - потому что заведение это внешняя система на чужом стеке.

---

## 11. Итог реализации: что сделано, что найдено, что осталось

### 11.1 Тестовое покрытие (фактическое)

| Пакет / уровень | Покрытие | Как проверено |
|---|---|---|
| `core/internal/domain` | 80% | конечный автомат статусов заказа (`CanTransition`) |
| `core/internal/usecase` | 67% | ручные моки репозиториев; создание заказа, переходы статусов, отмена, регрессия на баг с транзакцией (см. 11.2) |
| `core/internal/repository/postgres` | ~76.7%+ | `testcontainers-go`, реальный PostgreSQL 16 в Docker; непокрытые ~23% - тривиальный CRUD без развилок |
| `core/internal/transport/http/client` | ~95% | `httptest`, моки usecase на уровне интерфейсов — см. 11.4 |
| `core/internal/transport/http/*` | 0% | **не реализовано** |
| `restaurant-service/internal/coreclient` | 81% | `httptest.Server`, проверка метода/пути/заголовков/тела каждого запроса |
| `restaurant-service/internal/kitchen` | 80% | `httptest.Server` + подмена `time.Sleep` на мгновенную; полный цикл заказа, обрыв пайплайна при ошибке accept, работа поллера |
| `restaurant-service/internal/webhook` | 100% | валидный/невалидный payload |

### 11.2 HTTP-тесты хендлеров core — **[РЕАЛИЗОВАНО]**

Последний недостающий слой в пирамиде тестов. Чтобы это стало возможным
(тестировать хендлеры через `httptest` без поднятия реальной БД/usecase),
потребовался **небольшой рефакторинг** — не только добавление
тестов:

- `client.Handlers` и `venue.Handlers` раньше принимали конкретные
  `*usecase.CatalogUseCase`/`*usecase.OrderUseCase`/`*usecase.VenueMenuUseCase`.
  Теперь они принимают минимальные интерфейсы (`CatalogUseCase`,
  `OrderUseCase`, `MenuUseCase`), **объявленные в самих transport-пакетах**,
  а не в `usecase`. `main.go` при этом не поменялся ни строкой:
  конкретные типы `usecase`-слоя структурно (implicitly) удовлетворяют
  новым интерфейсам сами по себе.
- Тесты `client` (11 штук) покрывают весь положительный сценарий и основные ошибки:
  список заведений с фильтром, меню, создание заказа (включая `409` с
  деталями недоступных позиций), получение/отмена заказа, маппинг
  доменных ошибок в HTTP-статусы через `apierror.go`.
- Тесты `venue` (17 штук) — то же самое, но дополнительно идут через
  настоящий `middleware.VenueAuth` (с моком `domain.VenueRepository`), а
  не в обход авторизации — так тестируется полный реалистичный путь
  запроса, а не только бизнес-логика хендлера в изоляции. Включает `401`
  на отсутствующий/невалидный `X-API-Key`.
- Побочный эффект: тесты зафиксировали как контракт то, что раньше было
  только комментарием — что `venue.Order` (в отличие от `client.Order`) не
  содержит поле `items`, ровно как в спеке Venue API (см. 6.4, 11.3 п.4).

### 11.3 Баги, найденные в процессе

1. **FK-нарушение при создании заказа.** `AppendStatusHistory` писала запись
   истории статуса через `pool.Exec` (отдельное соединение), пока
   транзакция создания заказа ещё не закоммичена - падало на
   `order_status_history_order_id_fkey`. Найдено вручную через `curl`.
   **Фикс**: `AppendStatusHistory` теперь принимает опциональный
   `domain.Tx`. Закреплено регрессионными тестами (юнит + интеграционный).
2. **NULL-сканирование в `VenueRepo`.** `venues.description` - nullable
   колонка, Go-поле - обычный `string`. `callback_url` был обёрнут в
   `coalesce()`, а `description` - нет. Поймано интеграционным тестом на
   фикстуре без описания. **Фикс**: `coalesce()` добавлен симметрично.
3. **Самодедлок в собственном тесте на блокировку строк.** Тест на
   `FOR UPDATE` держал `tx1` открытой и внутри неё же вызывал
   `menuRepo.SetAvailability` (через пул, отдельным соединением) - тест
   зависал навсегда. **Фикс**: `tx1` переписан на сырую `pgx`-транзакцию.
4. **Расхождение спеки и кода в `venue.Order`.** До кодогенерации `venue`- и
   `client`-хендлеры делили один общий `OrderDTO` с полем `Items` - `GET
   /venue/orders` фактически отдавал лишнее `"items":null`, которого в
   OpenAPI-спеке Venue API никогда не было. После перехода на `oapi-codegen`
   каждый API получил собственный сгенерированный тип `Order`, в точности
   соответствующий своей спеке - расхождение исчезло структурно.
5. **Рассинхронизация версии Go по проекту.** `core/go.mod` в какой-то
   момент обновился до `go 1.25.0` (транзитивно, через `pgx`), а Docker-образы
   (`golang:1.22-alpine` в Dockerfile, `golang:1.22` в `test-docker`) и
   локальный тулчейн остались на 1.22 - сборка падала с разными симптомами
   в разных местах. **Фикс**: версия Go синхронизирована по всему проекту
   (1.25 везде - `go.mod`, оба `Dockerfile`, `docker-compose.yml`,
   `test-docker` в Makefile).

### 11.4 Что осталось (актуальный TODO)


- [ ] **CI (GitHub Actions)**: lint + unit-test + integration-test (нужен
  Docker в раннере, есть на `ubuntu-latest`) + build, отдельными job'ами
  для быстрой обратной связи.
- [ ] `/health` не проверяет соединение с БД - сейчас чистый liveness,
  стоило бы добавить readiness с пингом Postgres.
- [ ] Venue API: `POST/GET /menu-categories`, `PATCH /menu-items/{id}` (полное обновление).
- [ ] Персистентность `reason` при `POST /orders/{id}/reject`.
- [ ] `sqlc` для SQL-запросов репозиториев - кодогенерация закрыла только HTTP-слой.
- [ ] Расширение кодогенерации до серверных интерфейсов (`ServerInterface`) - сейчас генерируются только модели, роутинг остаётся ручным.

### 11.5 CI (GitHub Actions) — **[РЕАЛИЗОВАНО]**

`.github/workflows/ci.yml`, шесть job'ов:

| Job | Что делает |
|---|---|
| `lint` | `golangci-lint` для `core`/`restaurant-service` матрицей |
| `generate-check` | `make generate` + `git diff` по `*.gen.go` — ловит рассинхрон OpenAPI-спеки и сгенерированного кода |
| `unit-test` | `go test ./... -race -cover` для обоих модулей |
| `integration-test` | `testcontainers-go`, реальный Postgres (Docker есть на `ubuntu-latest` из коробки) |
| `build` | `go build ./...` для обоих модулей |
| `docker-smoke` | `docker compose up --wait` + E2E через `curl`/`jq`: список заведений → меню → заказ → получение заказа |

- **`docker-smoke` напрямую проверяет первый критерий приёмки из ТЗ** —
  "приложение поднимается и выполняет бизнес-сценарии" — не просто
  собирается, а создаёт заказ через живой стек и проверяет ответ.
- **`generate-check`** — не даёт кодогенерации быть не актуальной: если кто-то
  поправит `api/openapi/*.yaml` и забудет `make generate`, или, наоборот,
  отредактирует `*.gen.go` руками, CI укажет на diff и упадёт.