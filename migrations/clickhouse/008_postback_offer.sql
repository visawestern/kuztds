-- G-053: the offer a conversion belongs to (profit and cost per offer).
ALTER TABLE kuztds.postbacks ADD COLUMN IF NOT EXISTS offer String AFTER stream_name;
