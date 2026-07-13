// Error("msg") yields a constructed failure (the message); a custom error class
// without a message yields a sentinel (the class name); a re-throw yields a
// propagated failure (the forwarded identifier).
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
    throw err // re-throw — a propagated failure (source: err)
  }
}
