package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// CLI INTEGRATION TESTS
// =============================================================================

func skipIfNoMutool(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("mutool"); err != nil {
		t.Skip("mutool not available")
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "go-pdf-extractor")
	if isWindows() {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, output)
	}
	return binPath
}

func getPackageDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	return cwd
}

func getTestfilesDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	candidates := []string{
		filepath.Join(cwd, "testdata"),
		filepath.Join(cwd, "..", "..", "testdata"),
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(dir)
			return abs
		}
	}
	t.Skip("testdata directory not found")
	return ""
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}

// =============================================================================
// OUTPUT FORMAT TESTS
// =============================================================================

func TestCLI_JSONOutput(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-format", "json",
		"-output", outputFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}

	// Verify JSON is valid array
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, data)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Verify expected values
	values := make(map[string]string)
	for _, r := range results {
		filename := r["filename"].(string)
		if v, ok := r["value"].(string); ok {
			values[filename] = v
		}
	}

	if values["sample001.pdf"] != "Employee ID_X_X_X_X_Eag-AHP.pdf" {
		t.Errorf("sample001.pdf: expected 'Employee ID_X_X_X_X_Eag-AHP.pdf', got %q", values["sample001.pdf"])
	}
	if values["sample002.pdf"] != "327078_X_X_X_X_Wage.pdf" {
		t.Errorf("sample002.pdf: expected '327078_X_X_X_X_Wage.pdf', got %q", values["sample002.pdf"])
	}
}

func TestCLI_NDJSONOutput(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.ndjson")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-format", "ndjson",
		"-output", outputFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Errorf("line %d invalid JSON: %v", i, err)
		}
	}
}

func TestCLI_TSVOutput(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.tsv")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-format", "tsv",
		"-output", outputFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 { // header + 2 data rows
		t.Errorf("expected 3 lines (header + 2 data), got %d", len(lines))
	}

	// Verify header
	if lines[0] != "filename\tvalue" {
		t.Errorf("expected header 'filename\\tvalue', got %q", lines[0])
	}

	// Verify data rows have two columns
	for i, line := range lines[1:] {
		cols := strings.Split(line, "\t")
		if len(cols) != 2 {
			t.Errorf("data row %d: expected 2 columns, got %d", i+1, len(cols))
		}
	}
}

// =============================================================================
// DETECT MODE TESTS
// =============================================================================

func TestCLI_DetectMode_Success(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-detect",
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-output", outputFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("detect mode failed: %v\n%s", err, output)
	}

	outputStr := string(output)
	expectedChecks := []string{
		"[OK] Path readable",
		"[OK] File pattern matches",
		"[OK] Mutool found",
		"[OK] Mutool executes successfully",
		"[OK] Search pattern",
		"[OK] Output writable",
		"All prerequisite checks passed",
	}

	for _, check := range expectedChecks {
		if !strings.Contains(outputStr, check) {
			t.Errorf("expected output to contain %q", check)
		}
	}
}

func TestCLI_DetectMode_PathNotFound(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-detect",
		"-path", "/nonexistent/path/to/files",
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-output", outputFile,
	)
	output, _ := cmd.CombinedOutput()
	exitCode := cmd.ProcessState.ExitCode()

	if exitCode != 3 { // ExitPathError
		t.Errorf("expected exit code 3, got %d\n%s", exitCode, output)
	}
}

func TestCLI_DetectMode_NoMatchingFiles(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-detect",
		"-path", testdataDir,
		"-file-pattern", "*.nonexistent",
		"-search", "DSFN:",
		"-output", outputFile,
	)
	output, _ := cmd.CombinedOutput()
	exitCode := cmd.ProcessState.ExitCode()

	if exitCode != 6 { // ExitNoFilesFound
		t.Errorf("expected exit code 6, got %d\n%s", exitCode, output)
	}
}

func TestCLI_DetectMode_SearchNotFound(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-detect",
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "NONEXISTENT_PATTERN:",
		"-output", outputFile,
	)
	output, _ := cmd.CombinedOutput()
	exitCode := cmd.ProcessState.ExitCode()

	if exitCode != 7 { // ExitSearchNotFound
		t.Errorf("expected exit code 7, got %d\n%s", exitCode, output)
	}
}

// =============================================================================
// EXIT CODE TESTS
// =============================================================================

func TestCLI_ExitCode_Success(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-format", "json",
		"-output", outputFile,
	)
	_ = cmd.Run() // intentionally ignore; checking exit code
	exitCode := cmd.ProcessState.ExitCode()

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestCLI_ExitCode_MissingRequiredFlag(t *testing.T) {
	bin := buildBinary(t)

	cmd := exec.Command(bin,
		"-path", "/some/path",
		// missing -file-pattern, -search, -output
	)
	_ = cmd.Run() // intentionally ignore; checking exit code
	exitCode := cmd.ProcessState.ExitCode()

	if exitCode != 1 { // ExitConfigError
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestCLI_ExitCode_InvalidPath(t *testing.T) {
	bin := buildBinary(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-path", "/nonexistent/path",
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-output", outputFile,
	)
	_ = cmd.Run() // intentionally ignore; checking exit code
	exitCode := cmd.ProcessState.ExitCode()

	if exitCode != 3 { // ExitPathError
		t.Errorf("expected exit code 3, got %d", exitCode)
	}
}

func TestCLI_ExitCode_NoFilesFound(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "*.nonexistent",
		"-search", "DSFN:",
		"-output", outputFile,
	)
	_ = cmd.Run() // intentionally ignore; checking exit code
	exitCode := cmd.ProcessState.ExitCode()

	if exitCode != 6 { // ExitNoFilesFound
		t.Errorf("expected exit code 6, got %d", exitCode)
	}
}

// =============================================================================
// VERSION AND HELP TESTS
// =============================================================================

func TestCLI_Version(t *testing.T) {
	bin := buildBinary(t)

	cmd := exec.Command(bin, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version flag failed: %v", err)
	}

	if !strings.Contains(string(output), "go-pdf-extractor version") {
		t.Errorf("expected version output, got: %s", output)
	}
}

// =============================================================================
// WORKER FLAG TESTS
// =============================================================================

func TestCLI_WorkersFlag(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)

	workerCounts := []string{"1", "4", "8", "20"}

	for _, workers := range workerCounts {
		t.Run("workers="+workers, func(t *testing.T) {
			outputFile := filepath.Join(t.TempDir(), "output.json")

			cmd := exec.Command(bin,
				"-path", testdataDir,
				"-file-pattern", "sample*.pdf",
				"-search", "DSFN:",
				"-format", "json",
				"-output", outputFile,
				"-workers", workers,
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("CLI with workers=%s failed: %v\n%s", workers, err, output)
			}

			// Verify output is valid
			data, _ := os.ReadFile(outputFile)
			var results []map[string]interface{}
			if err := json.Unmarshal(data, &results); err != nil {
				t.Errorf("invalid output with workers=%s: %v", workers, err)
			}
			if len(results) != 2 {
				t.Errorf("workers=%s: expected 2 results, got %d", workers, len(results))
			}
		})
	}
}

// =============================================================================
// TIMEOUT FLAG TESTS
// =============================================================================

func TestCLI_TimeoutFlag(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "DSFN:",
		"-format", "json",
		"-output", outputFile,
		"-timeout", "60s",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI with timeout failed: %v\n%s", err, output)
	}

	// Verify output
	data, _ := os.ReadFile(outputFile)
	var results []map[string]interface{}
	if err := json.Unmarshal(data, &results); err != nil {
		t.Errorf("invalid output: %v", err)
	}
}

// =============================================================================
// SPECIFIC FILE PATTERN TESTS
// =============================================================================

func TestCLI_SpecificFilePattern(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "sample001.pdf",
		"-search", "DSFN:",
		"-format", "json",
		"-output", outputFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}

	data, _ := os.ReadFile(outputFile)
	var results []map[string]interface{}
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result for specific file, got %d", len(results))
	}
	if results[0]["filename"] != "sample001.pdf" {
		t.Errorf("expected sample001.pdf, got %v", results[0]["filename"])
	}
}

// =============================================================================
// NO MATCH TESTS
// =============================================================================

func TestCLI_NoMatchReturnsNullValues(t *testing.T) {
	skipIfNoMutool(t)
	bin := buildBinary(t)
	testdataDir := getTestfilesDir(t)
	outputFile := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(bin,
		"-path", testdataDir,
		"-file-pattern", "sample*.pdf",
		"-search", "NONEXISTENT_PATTERN:",
		"-format", "json",
		"-output", outputFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}

	data, _ := os.ReadFile(outputFile)
	var results []map[string]interface{}
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, r := range results {
		if r["value"] != nil {
			t.Errorf("expected null value for no match, got %v", r["value"])
		}
	}
}
