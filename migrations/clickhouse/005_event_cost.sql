-- Cost per event in USD (G-119…G-121; паритет Binom v2).
-- cost: цена клика по модели кампании; 0 = цена не задана или валюту
-- без курсов посчитать нельзя (движок пишет 0 и логирует, не смешивает валюты).
-- cost_cur: валюта модели для прозрачности (может быть пустой).
ALTER TABLE kuztds.events
    ADD COLUMN IF NOT EXISTS cost Float64,
    ADD COLUMN IF NOT EXISTS cost_cur LowCardinality(String);
