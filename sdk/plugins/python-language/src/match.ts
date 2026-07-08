/**
 * Matches Python source files.
 * Handles .py, .pyi (stub files), and .pyw (Windows GUI scripts).
 * Excludes common virtual environment and cache directories.
 */
export function match(filePath: string): boolean {
  const ext = filePath.substring(filePath.lastIndexOf("."))
  if (ext !== ".py" && ext !== ".pyi" && ext !== ".pyw") return false

  // Exclude virtual environments and caches
  if (filePath.includes("/venv/"))      return false
  if (filePath.includes("/.venv/"))     return false
  if (filePath.includes("/env/"))       return false
  if (filePath.includes("/__pycache__/")) return false
  if (filePath.includes("/site-packages/")) return false
  if (filePath.includes("/dist-packages/")) return false
  if (filePath.includes("/.tox/"))      return false
  if (filePath.includes("/.eggs/"))     return false

  return true
}
