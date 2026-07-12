package classify

import "fmt"

// Grade maps a score bucket to a letter — one behavior clause per case, plus the
// default as an "else" clause.
func Grade(score int) string {
	switch score {
	case 100:
		return "A+"
	case 90, 80:
		return "pass"
	default:
		return "fail"
	}
}

// Describe uses a subject-less switch (each case is its own boolean condition)
// and a terminal else on an if.
func Describe(n int) string {
	if n < 0 {
		return "negative"
	} else {
		fmt.Println("non-negative")
	}
	switch {
	case n == 0:
		return "zero"
	case n > 100:
		return "big"
	}
	return "small"
}

// Kind reports the dynamic type via a type switch.
func Kind(v any) string {
	switch v.(type) {
	case int:
		return "int"
	case string:
		return "string"
	default:
		return "other"
	}
}
