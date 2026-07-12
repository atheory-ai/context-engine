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
export type EffectConfidence = "high" | "low"

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
 * classifyEffect returns the (kind, confidence) for a detected effectful call.
 * A recognized category is high confidence; an uncategorizable call is
 * low-confidence "unclassified" (the comparator treats an undeclared
 * low-confidence effect as a warning rather than an error).
 */
export function classifyEffect(input: EffectClassifierInput): { kind: EffectKind; confidence: EffectConfidence } {
  const path = (input.importPath ?? "").toLowerCase()
  const root = (input.root ?? "").toLowerCase()
  const method = (input.method ?? "").toLowerCase()

  for (const [kind, prefixes] of pathCategories) {
    if (prefixes.some(p => path === p || path.startsWith(p + "/"))) return { kind, confidence: "high" }
  }
  for (const [kind, roots] of rootCategories) {
    if (roots.includes(root)) return { kind, confidence: "high" }
  }
  if (mutationVerbs.some(v => method.includes(v))) return { kind: "mutation", confidence: "high" }
  return { kind: "unclassified", confidence: "low" }
}
