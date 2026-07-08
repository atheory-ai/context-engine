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

export interface FunctionIntent {
  kind:         "FunctionIntent"
  name:         string
  language:     string
  visibility?:  IIRVisibility
  inputs:       IIRParam[]
  returns:      IIRReturn
  behavior:     IIRBehaviorClause[]
  sideEffects:  string[]
  failureModes: string[]
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
