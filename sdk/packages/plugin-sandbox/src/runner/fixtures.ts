import { readdirSync, readFileSync, statSync } from "fs"
import { join } from "path"

export interface Fixture {
  path:    string
  content: string
}

export function loadFixtures(fixturesDir: string): Fixture[] {
  const fixtures: Fixture[] = []

  try {
    const entries = readdirSync(fixturesDir)
    for (const entry of entries) {
      const fullPath = join(fixturesDir, entry)
      const stat = statSync(fullPath)
      if (stat.isFile()) {
        const content = readFileSync(fullPath, "utf8")
        fixtures.push({ path: entry, content })
      }
    }
  } catch (err) {
    const e = err as NodeJS.ErrnoException
    if (e.code !== "ENOENT") throw err
    // fixtures dir doesn't exist — return empty
  }

  return fixtures
}
