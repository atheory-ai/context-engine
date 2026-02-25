DROP INDEX IF EXISTS idx_tokens_revoked;
DROP INDEX IF EXISTS idx_tokens_scope;
DROP TABLE IF EXISTS tokens;

DROP INDEX IF EXISTS idx_project_paths_path;
DROP TABLE IF EXISTS project_paths;

DROP INDEX IF EXISTS idx_projects_status;
DROP INDEX IF EXISTS idx_projects_git_url;
DROP TABLE IF EXISTS projects;

DROP TABLE IF EXISTS schema_version;
