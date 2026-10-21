-- Conversion cost in USD (G-120: CPA/Revshare в момент конверсии).
-- cost: 0 = клик-модели (уже в events.cost), trash (выплата отменена) или
-- модель без цены. Учитывается в Performance: Profit = revenue − clickCost − convCost.
ALTER TABLE kuztds.postbacks
    ADD COLUMN IF NOT EXISTS cost Float64;
