BEGIN;
CREATE TABLE products (
    sku text PRIMARY KEY, name text NOT NULL, type text NOT NULL,
    price_minor bigint NOT NULL CHECK (price_minor > 0),
    currency text NOT NULL CHECK (currency = 'RUB'), active boolean NOT NULL DEFAULT true
);
CREATE TABLE orders (
    id text PRIMARY KEY, sku text NOT NULL REFERENCES products(sku),
    price_minor bigint NOT NULL CHECK (price_minor > 0), currency text NOT NULL CHECK (currency = 'RUB'),
    status text NOT NULL DEFAULT 'created' CHECK (status IN ('created','paid','delivering','delivered','payment_failed','out_of_stock','delivery_failed')),
    paid_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((status IN ('created','payment_failed') AND paid_at IS NULL) OR
           (status IN ('paid','delivering','delivered','out_of_stock','delivery_failed') AND paid_at IS NOT NULL))
);
CREATE TABLE payment_events (
    event_id text PRIMARY KEY, order_id text NOT NULL,
    status text NOT NULL CHECK (status IN ('paid','failed')),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0), currency text NOT NULL,
    created_at timestamptz NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
    processing_state text NOT NULL DEFAULT 'pending' CHECK (processing_state IN ('pending','waiting_order','applied','rejected','conflict')),
    next_attempt_at timestamptz NOT NULL DEFAULT now(), error text NOT NULL DEFAULT ''
);
CREATE INDEX payment_events_due ON payment_events(next_attempt_at, received_at)
    WHERE processing_state IN ('pending','waiting_order');
CREATE INDEX payment_events_order ON payment_events(order_id);
CREATE TABLE delivery_jobs (
    order_id text PRIMARY KEY REFERENCES orders(id),
    state text NOT NULL DEFAULT 'ready' CHECK (state IN ('ready','leased','done')),
    available_at timestamptz NOT NULL DEFAULT now(), leased_until timestamptz,
    lease_version bigint NOT NULL DEFAULT 0, attempts integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '', CHECK ((state = 'leased') = (leased_until IS NOT NULL))
);
CREATE INDEX delivery_jobs_due ON delivery_jobs(available_at) WHERE state = 'ready';
CREATE INDEX delivery_jobs_expired ON delivery_jobs(leased_until) WHERE state = 'leased';
CREATE TABLE delivery_operations (
    request_id text PRIMARY KEY, order_id text NOT NULL REFERENCES orders(id),
    supplier text NOT NULL, generation integer NOT NULL CHECK (generation > 0), sku text NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','unknown','issued','refused')),
    reason text NOT NULL DEFAULT '', UNIQUE(order_id, supplier, generation), UNIQUE(request_id, order_id)
);
CREATE TABLE deliveries (
    order_id text PRIMARY KEY REFERENCES orders(id), request_id text NOT NULL UNIQUE,
    supplier text NOT NULL, code text NOT NULL UNIQUE CHECK (code <> ''),
    delivered_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (request_id, order_id) REFERENCES delivery_operations(request_id, order_id)
);
INSERT INTO products(sku,name,type,price_minor,currency) VALUES
('STEAM-TOPUP-500','Пополнение Steam 500 ₽','topup',50000,'RUB'),
('STEAM-TOPUP-1000','Пополнение Steam 1000 ₽','topup',100000,'RUB'),
('STEAM-TOPUP-2500','Пополнение Steam 2500 ₽','topup',250000,'RUB'),
('KEY-CS2-PRIME','CS2 Prime Status ключ','key',129000,'RUB'),
('KEY-GTA5','GTA V ключ активации','key',199000,'RUB'),
('KEY-EFT','Escape from Tarkov ключ','key',349000,'RUB'),
('SUB-DISCORD-1M','Discord Nitro 1 месяц','subscription',39900,'RUB'),
('SUB-YT-3M','YouTube Premium 3 месяца','subscription',149000,'RUB'),
('SUB-SPOTIFY-1M','Spotify Premium 1 месяц','subscription',29900,'RUB'),
('GIFT-PSN-1000','PlayStation Store карта 1000 ₽','giftcard',100000,'RUB'),
('GIFT-XBOX-1500','Xbox Gift Card 1500 ₽','giftcard',150000,'RUB'),
('GIFT-ROBLOX-800','Roblox 800 Robux','giftcard',89000,'RUB');
COMMIT;
