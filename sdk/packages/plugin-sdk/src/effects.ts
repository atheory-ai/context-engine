// Shared side-effect classifier for language plugins. Given the structured parts
// of a detected call — the receiver root, the method, and (when the root is an
// imported package) its full import path — it returns the effect kind and a
// confidence. Plugins emit these on each SideEffect so the host doesn't have to
// re-derive them from the name string at compare time.
//
// Matching is structural (exact root, method substring, import-path prefix)
// rather than substring-on-the-whole-name, which avoids false hits like
// "catalog.save" being read as a log effect because the name contains "log".

export type EffectKind = "network" | "db" | "io" | "log" | "mutation" | "unclassified"
// How an effect's kind was established. "resolved" means it matched a known
// effectful API (an import path or a recognized client root) — deterministic
// knowledge, not a probabilistic guess. "heuristic" means it was inferred from a
// method-name verb or is uncategorized. The comparator treats an undeclared
// resolved effect as an error and an undeclared heuristic one as a warning: it
// should not fail verification on a guess.
export type EffectBasis = "resolved" | "heuristic"

export interface EffectClassifierInput {
  /** The called method / function name, e.g. "Get", "track". */
  method?: string
  /** The receiver root, e.g. "http", "analytics". */
  root?: string
  /** Full import path of the root when it is an imported package, e.g. "net/http". */
  importPath?: string
}

// Import-path prefixes are the strongest signal (a real, categorizable package).
const pathCategories: [EffectKind, string[]][] = [
  ["network", ["net/http", "net/url", "net", "google.golang.org/grpc", "github.com/gorilla/websocket"]],
  ["db", ["database/sql", "gorm.io", "go.mongodb.org", "github.com/redis", "github.com/jmoiron/sqlx"]],
  ["io", ["os", "io", "io/ioutil", "bufio", "path/filepath"]],
  // fmt's pure formatters (Sprintf, Errorf) are excluded before classification;
  // what reaches here (Println/Printf/Fprint…) writes output.
  ["log", ["log", "log/slog", "fmt"]],
]

// Receiver roots that categorize a call even without a resolved import path
// (e.g. member calls on a well-known client name).
const rootCategories: [EffectKind, string[]][] = [
  ["network", ["http", "https", "fetch", "axios", "request", "requests", "grpc", "ws", "socket", "urllib"]],
  ["db", ["db", "sql", "database", "redis", "mongo", "session", "repository", "datastore"]],
  ["io", ["os", "io", "fs", "ioutil", "file", "pathlib"]],
  ["log", ["log", "logger", "logging", "console", "slog", "fmt"]],
]

// Method names that signal an observable mutation/effect.
const mutationVerbs = ["track", "send", "emit", "publish", "save", "create", "update", "delete", "write"]

/**
 * classifyEffect returns the (kind, basis) for a detected effectful call. A call
 * matched against a known effectful API — by import path or a recognized client
 * root — is "resolved" (deterministic). A call categorized only from a
 * method-name verb, or left uncategorized, is "heuristic" (a guess).
 */
export function classifyEffect(input: EffectClassifierInput): { kind: EffectKind; basis: EffectBasis } {
  const path = (input.importPath ?? "").toLowerCase()
  const root = (input.root ?? "").toLowerCase()
  const method = (input.method ?? "").toLowerCase()

  for (const [kind, prefixes] of pathCategories) {
    if (prefixes.some(p => path === p || path.startsWith(p + "/"))) return { kind, basis: "resolved" }
  }
  for (const [kind, roots] of rootCategories) {
    if (roots.includes(root)) return { kind, basis: "resolved" }
  }
  if (mutationVerbs.some(v => method.includes(v))) return { kind: "mutation", basis: "heuristic" }
  return { kind: "unclassified", basis: "heuristic" }
}
