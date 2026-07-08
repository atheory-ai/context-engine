export interface ExpectedSymbol {
  name: string
  type: "function" | "type" | "interface" | "method" | "const" | "var" | "class" | "other"
  line: number
}

const ENUMERATORS: Record<string, (content: string) => ExpectedSymbol[]> = {

  ".go": (content: string): ExpectedSymbol[] => {
    const symbols: ExpectedSymbol[] = []
    const lines = content.split("\n")

    lines.forEach((line, i) => {
      const lineNum = i + 1
      const fnMatch = line.match(/^func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/)
      if (fnMatch) symbols.push({ name: fnMatch[1], type: "function", line: lineNum })

      const typeMatch = line.match(/^type\s+(\w+)\s+(?:struct|interface|func|\w)/)
      if (typeMatch) symbols.push({ name: typeMatch[1], type: "type", line: lineNum })

      const constMatch = line.match(/^\s{0,1}(\w+)\s*=/)
      if (constMatch && line.includes("const")) {
        symbols.push({ name: constMatch[1], type: "const", line: lineNum })
      }
    })

    return symbols
  },

  ".ts": (content: string): ExpectedSymbol[] => {
    const symbols: ExpectedSymbol[] = []
    const lines = content.split("\n")

    lines.forEach((line, i) => {
      const lineNum = i + 1
      const fnMatch = line.match(/^(?:export\s+)?(?:async\s+)?function\s+(\w+)/)
      if (fnMatch) symbols.push({ name: fnMatch[1], type: "function", line: lineNum })

      const classMatch = line.match(/^(?:export\s+)?class\s+(\w+)/)
      if (classMatch) symbols.push({ name: classMatch[1], type: "class", line: lineNum })

      const ifaceMatch = line.match(/^(?:export\s+)?interface\s+(\w+)/)
      if (ifaceMatch) symbols.push({ name: ifaceMatch[1], type: "interface", line: lineNum })

      const typeMatch = line.match(/^(?:export\s+)?type\s+(\w+)\s*=/)
      if (typeMatch) symbols.push({ name: typeMatch[1], type: "type", line: lineNum })
    })

    return symbols
  },

  ".py": (content: string): ExpectedSymbol[] => {
    const symbols: ExpectedSymbol[] = []
    const lines = content.split("\n")

    lines.forEach((line, i) => {
      const lineNum = i + 1
      const fnMatch = line.match(/^(?:async\s+)?def\s+(\w+)\s*\(/)
      if (fnMatch) symbols.push({ name: fnMatch[1], type: "function", line: lineNum })

      const classMatch = line.match(/^class\s+(\w+)/)
      if (classMatch) symbols.push({ name: classMatch[1], type: "class", line: lineNum })
    })

    return symbols
  },
}

export function enumerateExpectedSymbols(
  filePath: string,
  content: string,
): ExpectedSymbol[] | null {
  const ext = filePath.substring(filePath.lastIndexOf("."))
  const enumerator = ENUMERATORS[ext]
  if (!enumerator) return null
  return enumerator(content)
}
