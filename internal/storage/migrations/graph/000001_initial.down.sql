DROP INDEX IF EXISTS idx_enrichments_turn;
DROP INDEX IF EXISTS idx_enrichments_entity;
DROP INDEX IF EXISTS idx_enrichments_run;
DROP TABLE IF EXISTS enrichments;

DROP INDEX IF EXISTS idx_index_runs_started;
DROP INDEX IF EXISTS idx_index_runs_status;
DROP INDEX IF EXISTS idx_index_runs_project;
DROP TABLE IF EXISTS index_runs;

DROP INDEX IF EXISTS idx_concept_seeds_source;
DROP INDEX IF EXISTS idx_concept_seeds_term;
DROP TABLE IF EXISTS concept_seeds;

DROP INDEX IF EXISTS idx_edge_weight_source_class;
DROP INDEX IF EXISTS idx_edge_weight_weight;
DROP TABLE IF EXISTS edge_weight;

DROP INDEX IF EXISTS idx_edges_target_type;
DROP INDEX IF EXISTS idx_edges_source_type;
DROP INDEX IF EXISTS idx_edges_source_class;
DROP INDEX IF EXISTS idx_edges_type;
DROP INDEX IF EXISTS idx_edges_target;
DROP INDEX IF EXISTS idx_edges_source;
DROP INDEX IF EXISTS idx_edges_project;
DROP TABLE IF EXISTS edges;

DROP INDEX IF EXISTS idx_node_activation_level;
DROP TABLE IF EXISTS node_activation;

DROP INDEX IF EXISTS idx_nodes_plugin;
DROP INDEX IF EXISTS idx_nodes_source_class;
DROP INDEX IF EXISTS idx_nodes_label;
DROP INDEX IF EXISTS idx_nodes_canonical;
DROP INDEX IF EXISTS idx_nodes_type;
DROP INDEX IF EXISTS idx_nodes_project;
DROP TABLE IF EXISTS nodes;

DROP TABLE IF EXISTS schema_version;
