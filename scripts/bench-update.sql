-- Mengukur UPDATE seluruh row tanpa batching pada dataset dari cmd/seed-large. Data tidak berubah (ROLLBACK).
-- Jalankan: psql "$TARGET_ADMIN_DATABASE_URL" -f scripts/bench-update.sql
\timing on
SELECT count(*) AS total_rows FROM public.orders;
BEGIN;
UPDATE public.orders SET amount = amount * 1.01, updated_at = now();
ROLLBACK;
