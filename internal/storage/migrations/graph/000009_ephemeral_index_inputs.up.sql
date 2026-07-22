-- Source and CST are index-time inputs, not default durable graph state.
-- File hashes plus the extractor fingerprint determine whether a contribution
-- can be reused; derived graph facts remain the durable index product.
DROP TABLE IF EXISTS index_parse_artifacts;
DROP TABLE IF EXISTS index_source_artifacts;

ALTER TABLE index_runs ADD COLUMN extractor_fingerprint TEXT NOT NULL DEFAULT '';
