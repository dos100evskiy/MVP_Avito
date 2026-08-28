-- Требуется для gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE venues (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL,
    description   text,
    city          text NOT NULL,
    is_active     boolean NOT NULL DEFAULT true,
    api_key_hash  text NOT NULL UNIQUE,
    callback_url  text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE menu_categories (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id   uuid NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    name       text NOT NULL,
    sort_order int NOT NULL DEFAULT 0
);
CREATE INDEX idx_menu_categories_venue ON menu_categories(venue_id);

CREATE TABLE menu_items (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id     uuid NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    category_id  uuid REFERENCES menu_categories(id) ON DELETE SET NULL,
    name         text NOT NULL,
    description  text,
    price_kopecks  bigint NOT NULL CHECK (price_kopecks >= 0),
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
    total_kopecks  bigint NOT NULL CHECK (total_kopecks >= 0),
    currency     char(3) NOT NULL DEFAULT 'RUB',
    customer_ref text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_orders_venue_status ON orders(venue_id, status);
CREATE INDEX idx_orders_created_at ON orders(created_at);

CREATE TABLE order_items (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id       uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    menu_item_id   uuid NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    name_snapshot  text NOT NULL,
    price_snapshot bigint NOT NULL,
    quantity       int NOT NULL CHECK (quantity > 0)
);
CREATE INDEX idx_order_items_order ON order_items(order_id);

CREATE TABLE order_status_history (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    from_status varchar(20),
    to_status   varchar(20) NOT NULL,
    changed_at  timestamptz NOT NULL DEFAULT now(),
    changed_by  varchar(20) NOT NULL
);
CREATE INDEX idx_order_status_history_order ON order_status_history(order_id);
