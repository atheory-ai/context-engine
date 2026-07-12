// grade maps a numeric bucket to a label via a switch — one behavior clause per
// case, plus default as an "else" clause.
export function grade(score: number): string {
  switch (score) {
    case 100:
      return "perfect"
    case 0:
      return "zero"
    default:
      return "other"
  }
}

// describe exercises a terminal else on an if.
export function describe(n: number): string {
  if (n < 0) {
    return "negative"
  } else {
    return "non-negative"
  }
}
