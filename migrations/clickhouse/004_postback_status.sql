-- Conversion status + transaction id (G-301…G-304, паритет Keitaro 11).
-- status: lead/sale/rejected/registration/deposit/trash + custom (валидирует движок).
-- tid: номер сделки против дублей (может быть пустым).
-- Аддитивная миграция: старые строки получают пустые значения.
ALTER TABLE kuztds.postbacks
    ADD COLUMN IF NOT EXISTS status LowCardinality(String),
    ADD COLUMN IF NOT EXISTS tid String;
