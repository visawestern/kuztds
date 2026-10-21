-- G-048: the offer a visit was sent to (empty when the stream served a URL).
ALTER TABLE kuztds.events ADD COLUMN IF NOT EXISTS offer String AFTER stream_name;
