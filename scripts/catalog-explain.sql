-- Capture the actual output with psql after loading catalog-fixture.sql.
EXPLAIN (ANALYZE, BUFFERS)
SELECT sku,name,type,price_minor,currency
FROM products
WHERE active
ORDER BY sku;
