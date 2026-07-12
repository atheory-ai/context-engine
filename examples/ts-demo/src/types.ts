// Domain types for the demo. The verifier parses each source file on its own,
// so these exist mainly to make the example a coherent, readable TS project.
export interface Money {
  cents: number
  currency: string
}

export interface Campaign {
  id: string
  minimumCents: number
}

export type ValidationResult<T> =
  | { ok: true; value: T }
  | { ok: false; error: string }
