// Custom error classes without a string message must still yield a failure mode
// (the class name); Error("msg") keeps yielding the message; a re-throw is skipped.
class NotFoundError extends Error {}

export function loadUser(id: string): string {
  if (id === "") {
    throw new Error("empty_id")
  }
  if (id === "missing") {
    throw new NotFoundError()
  }
  return id
}

export function retry(fn: () => void): void {
  try {
    fn()
  } catch (err) {
    throw err // re-throw — no stable name, not a declared failure mode
  }
}
