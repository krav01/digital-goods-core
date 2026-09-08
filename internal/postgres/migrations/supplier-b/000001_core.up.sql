BEGIN;
CREATE TABLE issue_requests (
    request_id text PRIMARY KEY, order_id text NOT NULL, sku text NOT NULL,
    outcome text NOT NULL CHECK (outcome IN ('pending','issued','refused')),
    code text UNIQUE, reason text NOT NULL DEFAULT '',
    CHECK ((outcome = 'issued') = (code IS NOT NULL))
);
CREATE UNIQUE INDEX issue_requests_one_per_order ON issue_requests(order_id) WHERE outcome = 'issued';
CREATE TABLE inventory_keys (
    code text PRIMARY KEY, sku text NOT NULL,
    issued_request_id text UNIQUE REFERENCES issue_requests(request_id)
);
CREATE INDEX inventory_available ON inventory_keys(sku,code) WHERE issued_request_id IS NULL;
INSERT INTO inventory_keys(code,sku) VALUES
('B-STEAM-500-01','STEAM-TOPUP-500'),('B-STEAM-1000-01','STEAM-TOPUP-1000'),
('B-STEAM-2500-01','STEAM-TOPUP-2500'),('B-CS2-01','KEY-CS2-PRIME'),
('B-GTA5-01','KEY-GTA5'),('B-EFT-01','KEY-EFT'),
('B-DISCORD-01','SUB-DISCORD-1M'),('B-YT-01','SUB-YT-3M'),
('B-SPOTIFY-01','SUB-SPOTIFY-1M'),('B-PSN-01','GIFT-PSN-1000'),
('B-XBOX-01','GIFT-XBOX-1500'),('B-ROBLOX-01','GIFT-ROBLOX-800'),
('B-STEAM-500-02','STEAM-TOPUP-500'),('B-STEAM-1000-02','STEAM-TOPUP-1000'),
('B-STEAM-2500-02','STEAM-TOPUP-2500'),('B-CS2-02','KEY-CS2-PRIME'),
('B-GTA5-02','KEY-GTA5'),('B-EFT-02','KEY-EFT'),
('B-DISCORD-02','SUB-DISCORD-1M'),('B-YT-02','SUB-YT-3M'),
('B-SPOTIFY-02','SUB-SPOTIFY-1M'),('B-PSN-02','GIFT-PSN-1000'),
('B-XBOX-02','GIFT-XBOX-1500'),('B-ROBLOX-02','GIFT-ROBLOX-800'),
('B-STEAM-500-03','STEAM-TOPUP-500');
COMMIT;
