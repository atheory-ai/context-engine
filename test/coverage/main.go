package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type packageResult struct {
	Package  string  `json:"package"`
	Coverage float64 `json:"coverage"`
	Minimum  float64 `json:"minimum"`
	NoStmts  bool    `json:"no_statements"`
	Passed   bool    `json:"passed"`
}

var coveragePattern = regexp.MustCompile(`coverage: ([0-9]+(?:\.[0-9]+)?)% of statements`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	module, err := goListModule()
	if err != nil {
		return err
	}
	packages, err := goListPackages()
	if err != nil {
		return err
	}
	minimums, err := loadMinimums(filepath.Join("test", "coverage", "minimums.json"))
	if err != nil {
		return err
	}

	outDir := "coverage"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create coverage dir: %w", err)
	}

	var combined bytes.Buffer
	combined.WriteString("mode: atomic\n")

	results := make([]packageResult, 0, len(packages))
	var failures []packageResult

	for _, pkg := range packages {
		rel := relativePackage(module, pkg)
		profilePath := filepath.Join(outDir, "pkg-"+hashString(pkg)+".out")
		output, err := goTestCover(pkg, profilePath)
		if err != nil {
			return fmt.Errorf("coverage test failed for %s:\n%s\n%w", rel, output, err)
		}

		result := packageResult{
			Package: rel,
			Minimum: minimumFor(rel, minimums),
			Passed:  true,
		}
		if strings.Contains(output, "coverage: [no statements]") {
			result.NoStmts = true
		} else {
			coverage, err := parseCoverage(output)
			if err != nil {
				return fmt.Errorf("parse coverage for %s from output:\n%s", rel, output)
			}
			result.Coverage = coverage
			if coverage+0.0001 < result.Minimum {
				result.Passed = false
				failures = append(failures, result)
			}
			if err := appendProfile(&combined, profilePath); err != nil {
				return err
			}
		}
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Package < results[j].Package
	})

	if err := os.WriteFile(filepath.Join(outDir, "unit.out"), combined.Bytes(), 0644); err != nil {
		return fmt.Errorf("write combined coverage profile: %w", err)
	}
	if err := writeJSON(filepath.Join(outDir, "coverage.json"), results); err != nil {
		return err
	}
	report := markdownReport(results)
	if err := os.WriteFile(filepath.Join(outDir, "coverage.md"), []byte(report), 0644); err != nil {
		return fmt.Errorf("write coverage report: %w", err)
	}
	if summaryPath := os.Getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open GitHub step summary: %w", err)
		}
		if _, err := f.WriteString(report); err != nil {
			_ = f.Close()
			return fmt.Errorf("write GitHub step summary: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close GitHub step summary: %w", err)
		}
	}

	fmt.Print(report)
	if len(failures) > 0 {
		return fmt.Errorf("%d package(s) below coverage minimum", len(failures))
	}
	return nil
}

func goListModule() (string, error) {
	cmd := exec.Command("go", "list", "-m")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func goListPackages() ([]string, error) {
	cmd := exec.Command("go", "list", "./...")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list ./...: %w", err)
	}
	var packages []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		pkg := strings.TrimSpace(scanner.Text())
		switch {
		case pkg == "":
			continue
		case strings.HasSuffix(pkg, "/test/acceptance"):
			continue
		case strings.HasSuffix(pkg, "/test/coverage"):
			continue
		default:
			packages = append(packages, pkg)
		}
	}
	return packages, scanner.Err()
}

func goTestCover(pkg, profilePath string) (string, error) {
	cmd := exec.Command("go", "test", "-covermode=atomic", "-coverprofile="+profilePath, pkg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func parseCoverage(output string) (float64, error) {
	matches := coveragePattern.FindStringSubmatch(output)
	if len(matches) != 2 {
		return 0, fmt.Errorf("coverage percentage not found")
	}
	return strconv.ParseFloat(matches[1], 64)
}

func appendProfile(dst *bytes.Buffer, profilePath string) error {
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("read coverage profile %s: %w", profilePath, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode: ") || strings.TrimSpace(line) == "" {
			continue
		}
		dst.WriteString(line)
		dst.WriteByte('\n')
	}
	return scanner.Err()
}

func loadMinimums(path string) (map[string]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read coverage minimums: %w", err)
	}
	var minimums map[string]float64
	if err := json.Unmarshal(data, &minimums); err != nil {
		return nil, fmt.Errorf("parse coverage minimums: %w", err)
	}
	return minimums, nil
}

func minimumFor(pkg string, minimums map[string]float64) float64 {
	if minimum, ok := minimums[pkg]; ok {
		return minimum
	}
	return minimums["default"]
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func markdownReport(results []packageResult) string {
	var b strings.Builder
	b.WriteString("## Coverage\n\n")
	b.WriteString("| Package | Coverage | Minimum | Status |\n")
	b.WriteString("| --- | ---: | ---: | --- |\n")
	for _, result := range results {
		coverage := fmt.Sprintf("%.1f%%", result.Coverage)
		if result.NoStmts {
			coverage = "n/a"
		}
		status := "pass"
		if !result.Passed {
			status = "fail"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %.1f%% | %s |\n",
			result.Package, coverage, result.Minimum, status)
	}
	b.WriteByte('\n')
	return b.String()
}

func relativePackage(module, pkg string) string {
	if pkg == module {
		return "."
	}
	return strings.TrimPrefix(strings.TrimPrefix(pkg, module), "/")
}

func hashString(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:8])
}
