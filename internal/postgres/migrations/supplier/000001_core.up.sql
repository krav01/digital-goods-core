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
-- Supplier A uses even-numbered entries (zero-based) of the original 50-key pool.
-- The other 25 entries are reserved for B. SKU allocation is deterministic.
INSERT INTO inventory_keys(code,sku) VALUES
('LFXC-TNCS-BPCD','STEAM-TOPUP-500'),('FEL3-GUXN-TCCH','STEAM-TOPUP-1000'),
('0K9E-P1FR-BY1U','STEAM-TOPUP-2500'),('X93K-NYAQ-GEC1','KEY-CS2-PRIME'),
('M58F-GIIR-VJAP','KEY-GTA5'),('OODW-CCHF-MBAF','KEY-EFT'),
('QRDD-MJ3F-A8TF','SUB-DISCORD-1M'),('LI39-4330-ISMB','SUB-YT-3M'),
('HHW6-4RX2-DX62','SUB-SPOTIFY-1M'),('EF63-F39X-MTEA','GIFT-PSN-1000'),
('JPE6-MQV6-P7ST','GIFT-XBOX-1500'),('T2DU-IJ1S-U16P','GIFT-ROBLOX-800'),
('U74E-EPCI-CY26','STEAM-TOPUP-500'),('FPSM-HLZA-TPAL','STEAM-TOPUP-1000'),
('P63J-F7UZ-DCYP','STEAM-TOPUP-2500'),('JESI-DFBH-LK1K','KEY-CS2-PRIME'),
('3PR4-OSY9-M3ZW','KEY-GTA5'),('KIKQ-FQJ8-9TI8','KEY-EFT'),
('BAKI-VT1X-Z5OL','SUB-DISCORD-1M'),('S423-V6YY-IBEM','SUB-YT-3M'),
('XC0J-CJ0H-09RN','SUB-SPOTIFY-1M'),('CJYY-YKSQ-QE6H','GIFT-PSN-1000'),
('FS8E-3S5Z-I6RA','GIFT-XBOX-1500'),('7Z6K-NO9V-MPJB','GIFT-ROBLOX-800'),
('W67T-ZB0Q-1XKB','STEAM-TOPUP-500');
COMMIT;
