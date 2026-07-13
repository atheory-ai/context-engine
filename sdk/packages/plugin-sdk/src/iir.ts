// The IIR (Intermediate Intent Representation) model, mirrored from Context
// Engine's internal/iir. A language plugin may *lift* source into these types
// during extraction; the host validates and stores them (it remains the
// authoritative parser — these are the authoring/contract surface). Field names
// match the host's JSON tags so a host-side ParseIntentJSON accepts them.

export type IIRVisibility = "public" | "private"

// TypeUnknown marks a parameter type that could not be determined from source —
// represented explicitly rather than dropped, so comparison can tell "unknown"
// from "absent".
export const IIRTypeUnknown = "unknown"

export interface IIRParam {
  name: string
  type: string
}

export interface IIRReturn {
  // Empty string means the return type was absent in source (distinct from the
  // explicit type "void").
  type:     string
  explicit: boolean
}

// IIRExpr is a normalized condition expression node (mirrors iir.Expr): `op`
// names the node ("<", "&&", "!", "path", "lit"), `args` are operands in source
// order, `text` carries a leaf payload (a literal value or a dotted access path).
export interface IIRExpr {
  op:    string
  args?: IIRExpr[]
  text?: string
}

export interface IIRBehaviorClause {
  when:      string
  then:      string
  whenExpr?: IIRExpr
}

/**
 * An observable side effect. Either a bare name ("analytics.track") or an object
 * carrying an optional kind (network | db | io | log | mutation | unclassified)
 * and basis — "resolved" when the kind came from a known effectful API (an
 * import path or recognized client), "heuristic" when it was guessed from a
 * method-name verb. The host accepts both forms; a plugin that can categorize an
 * effect emits the object form, otherwise a bare string.
 */
export type IIRSideEffect = string | {
  name:  string
  kind?: "network" | "db" | "io" | "log" | "mutation" | "unclassified"
  basis?: "resolved" | "heuristic"
}

/**
 * An expected failure outcome. Either a bare code ("amount_below_minimum") or an
 * object carrying an optional kind and source. The kind names *how* the function
 * signals the failure: "constructed" (created inline, e.g. throw new Error("msg")
 * / errors.New), "sentinel" (a named error value/type, e.g. ErrClosed or a custom
 * error class), or "propagated" (an upstream failure forwarded on — a re-throw or
 * `return nil, err`). For a propagated failure, `source` names the forwarded
 * identifier. The host accepts both forms; a code-only failure round-trips as a
 * bare string.
 */
export type IIRFailureMode = string | {
  code:    string
  kind?:   "constructed" | "sentinel" | "propagated"
  source?: string
}

export interface FunctionIntent {
  kind:         "FunctionIntent"
  name:         string
  language:     string
  visibility?:  IIRVisibility
  inputs:       IIRParam[]
  returns:      IIRReturn
  behavior:     IIRBehaviorClause[]
  sideEffects:  IIRSideEffect[]
  failureModes: IIRFailureMode[]
  constraints:  string[]
}

// ExtractedFunction pairs a lifted FunctionIntent with the id of the symbol node
// it was lifted from. The plugin knows this id directly (it created the node),
// so the host can attach the IIR to the exact node without the (name,start_byte)
// correlation the Go-side lift needs.
export interface ExtractedFunction {
  nodeId: string
  intent: FunctionIntent
}
