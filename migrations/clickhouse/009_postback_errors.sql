-- G-066: why an incoming postback was not recorded.
-- Diagnostic only; the query is stripped of the postback key before insert.
CREATE TABLE IF NOT EXISTS kuztds.postback_errors
(
    ts     DateTime DEFAULT now(),
    reason LowCardinality(String),
    cid    String,
    status LowCardinality(String),
    tid    String,
    ip     String,
    query  String,
    detail String
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(ts)
ORDER BY ts
TTL ts + INTERVAL 90 DAY;
