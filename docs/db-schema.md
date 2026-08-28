# Схема БД

Подробное описание — см. миграции в `core/migrations/000001_init.up.sql`
(источник истины) и раздел плана `avito-kuhnya-plan.md`.

## Таблицы

- **venues** — заведения, подключённые к платформе. `api_key_hash` — sha256
  от статического API-ключа заведения (упрощённая авторизация для MVP).
- **menu_categories** — категории меню, принадлежат заведению.
- **menu_items** — позиции меню. Цена — `price_Kopecks` (bigint, минимальные
  единицы валюты, без чисел с плавающей точкой).
- **orders** — заказы. `status` — `varchar + CHECK` (не Postgres ENUM),
  чтобы проще добавлять новые статусы при масштабировании без блокирующих
  `ALTER TYPE`.
- **order_items** — позиции заказа со **снимком** цены/названия на момент
  заказа (`price_snapshot`, `name_snapshot`) — история заказа не должна
  меняться при изменении меню задним числом.
- **order_status_history** — журнал смены статусов, для аудита и отладки.

## Связи

```
venues (1) ──< menu_categories (1) ──< menu_items
venues (1) ──< orders (1) ──< order_items >── menu_items
orders (1) ──< order_status_history
```

## Индексы

- `menu_items(venue_id, is_available)` — быстрый листинг доступных позиций.
- `orders(venue_id, status)` — для poll-эндпоинта заведения.
- `orders(created_at)` — для пагинации/сортировки.
