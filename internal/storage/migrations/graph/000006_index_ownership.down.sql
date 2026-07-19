DROP TABLE IF EXISTS index_file_iir;
DROP TABLE IF EXISTS index_file_edges;
DROP TABLE IF EXISTS index_file_nodes;
DROP INDEX IF EXISTS idx_nodes_index_run;
DROP INDEX IF EXISTS idx_edges_index_run;
ALTER TABLE nodes DROP COLUMN index_managed;
ALTER TABLE nodes DROP COLUMN last_index_run_id;
ALTER TABLE edges DROP COLUMN index_managed;
ALTER TABLE edges DROP COLUMN last_index_run_id;
