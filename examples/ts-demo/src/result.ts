import type { ValidationResult } from "./types"

export function ok<T>(value: T): ValidationResult<T> {
  return { ok: true, value }
}

export function err<T>(error: string): ValidationResult<T> {
  return { ok: false, error }
}
