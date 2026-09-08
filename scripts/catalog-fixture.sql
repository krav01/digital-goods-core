-- Run only against a disposable application database after migrations.
-- The generated SKU range is deterministic and does not modify the 12 base products.
INSERT INTO products(sku,name,type,price_minor,currency,active)
SELECT format('FIXTURE-SKU-%05s', n), format('Fixture product %s', n), 'fixture',
       10000 + n, 'RUB', true
FROM generate_series(1, 10000) AS n
ON CONFLICT (sku) DO NOTHING;

ANALYZE products;
