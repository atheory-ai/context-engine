package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// promptString reads a single line of input from stdin.
func promptString(prompt string) string {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// promptMultiline reads multiple lines until two consecutive empty lines.
func promptMultiline(prompt string) string {
	fmt.Print(prompt)
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	emptyCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			emptyCount++
			if emptyCount >= 2 {
				break
			}
		} else {
			emptyCount = 0
		}
		lines = append(lines, line)
	}
	// Trim trailing empty lines
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// promptChoice reads input and validates against allowed choices.
// Returns defaultVal if input is empty.
func promptChoice(prompt string, choices []string, defaultVal string) string {
	for {
		fmt.Print(prompt)
		scanner := bufio.NewScanner(os.Stdin)
		var input string
		if scanner.Scan() {
			input = strings.TrimSpace(scanner.Text())
		}
		if input == "" {
			return defaultVal
		}
		for _, c := range choices {
			if input == c {
				return input
			}
		}
		fmt.Printf("Invalid choice %q. Options: %s\n", input, strings.Join(choices, ", "))
	}
}

// promptYesNo reads a yes/no response. Returns defaultVal for empty input.
func promptYesNo(prompt string, defaultVal bool) bool {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		input := strings.TrimSpace(strings.ToLower(scanner.Text()))
		switch input {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
	return defaultVal
}
