package iir

// Shared constructors for building normalized Expr trees in tests.

func path(s string) *Expr             { return &Expr{Op: "path", Text: s} }
func lit(s string) *Expr              { return &Expr{Op: "lit", Text: s} }
func bin(op string, l, r *Expr) *Expr { return &Expr{Op: op, Args: []*Expr{l, r}} }
