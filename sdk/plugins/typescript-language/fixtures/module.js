/**
 * Pure JavaScript module — no TypeScript or JSX.
 * Tests that the plugin handles .js files correctly.
 */

import { EventEmitter } from "events"
import path from "path"

const DEFAULT_TIMEOUT = 5000
const MAX_RETRIES = 3

export class Queue extends EventEmitter {
  constructor(options = {}) {
    super()
    this.timeout = options.timeout ?? DEFAULT_TIMEOUT
    this.retries = options.retries ?? MAX_RETRIES
    this._items = []
    this._processing = false
  }

  enqueue(item) {
    this._items.push(item)
    this.emit("enqueue", item)
    if (!this._processing) {
      this._process()
    }
    return this
  }

  async _process() {
    this._processing = true
    while (this._items.length > 0) {
      const item = this._items.shift()
      try {
        await this._handle(item)
        this.emit("done", item)
      } catch (err) {
        this.emit("error", err, item)
      }
    }
    this._processing = false
  }

  async _handle(item) {
    throw new Error("Not implemented")
  }

  get size() {
    return this._items.length
  }
}

export const createQueue = (options) => new Queue(options)

export function resolveModulePath(base, relative) {
  return path.resolve(path.dirname(base), relative)
}

const cache = new Map()

export async function loadModule(modulePath) {
  if (cache.has(modulePath)) {
    return cache.get(modulePath)
  }
  const mod = await import(modulePath)
  cache.set(modulePath, mod)
  return mod
}

export function memoize(fn) {
  const memo = new Map()
  return function (...args) {
    const key = JSON.stringify(args)
    if (memo.has(key)) return memo.get(key)
    const result = fn.apply(this, args)
    memo.set(key, result)
    return result
  }
}
