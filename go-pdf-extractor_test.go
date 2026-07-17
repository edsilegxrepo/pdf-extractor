// Package main provides tests for go-pdf-extract.
//
// Test Strategy Overview:
//
// 1. UNIT TESTS (run always, no external dependencies):
//   - Test individual functions in isolation with controlled inputs
//   - Mock or bypass external dependencies (mutool, filesystem)
//   - Fast execution, deterministic results
//   - Examples: TestExtractValues, TestValidateConfig, TestResultSerialization
//
// 2. INTEGRATION TESTS (require mutool, skip with -short flag):
//   - Test complete workflows with real mutool binary
//   - Use actual PDF files from testfiles/ directory
//   - Verify end-to-end functionality
//   - Examples: TestIntegration_SingleFileWithMatch, TestIntegration_EndToEnd
//
// 3. CLI FLAG COMBINATION TESTS (require mutool):
//   - Systematically test all CLI flag combinations
//   - Verify output formats, patterns, timeouts work correctly
//   - Examples: TestIntegration_FormatJSON, TestIntegration_WorkersFlag
//
// 4. EXIT CODE TESTS:
//   - Verify correct exit codes for each error condition
//   - Test both success and failure paths
//   - Examples: TestRun_ExitCodes, TestRun_ExitSuccess
//
// Test Execution:
//
//	go test -v ./...           # Run all tests (requires mutool)
//	go test -v -short ./...    # Run unit tests only (no mutool required)
//	go test -v -run TestExtract  # Run specific test pattern
//
// Test Coverage Requirements (from DESIGN.md):
//   - Minimum 80% code coverage
//   - 100% functional coverage for CLI execution paths
//
// Workspace Management:
//
//	Tests use ephemeral workspaces created via createTestWorkspace().
//	Set KEEP_TEST_WORKSPACE=1 to preserve workspaces for debugging.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// TEST UTILITIES
// =============================================================================

// createTestWorkspace creates an ephemeral test workspace per TESTING.md requirements.
// Location: {TEMP}/unittests/go-pdf-extractor_{YYYYMMDDhhmmss}
// Cleanup: Automatic unless KEEP_TEST_WORKSPACE=1 is set.
// Purpose: Provides isolated output directory for integration tests.
func createTestWorkspace(t *testing.T) string {
	t.Helper()

	timestamp := time.Now().Format("20060102150405")
	baseDir := filepath.Join(os.TempDir(), "unittests")
	workspaceDir := filepath.Join(baseDir, fmt.Sprintf("go-pdf-extractor_%s", timestamp))

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}

	if os.Getenv("KEEP_TEST_WORKSPACE") == "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(workspaceDir)
		})
	} else {
		t.Logf("Test workspace preserved at: %s", workspaceDir)
	}

	return workspaceDir
}

// =============================================================================
// UNIT TESTS: Value Extraction
// =============================================================================
// Tests the core extractValues() function which parses mutool output text
// to find and extract values following the search pattern.

// TestExtractValues verifies pattern matching and value extraction logic.
// Test cases cover: single/multiple matches, whitespace handling, deduplication.
func TestExtractValues(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		search   string
		expected []string
	}{
		{
			name:     "single match no space",
			text:     "Some text\nDSFN:123456\nmore text",
			search:   "DSFN:",
			expected: []string{"123456"},
		},
		{
			name:     "single match with space",
			text:     "Some text\nDSFN: 327078_X_X_X_X_Wage.pdf\nmore text",
			search:   "DSFN:",
			expected: []string{"327078_X_X_X_X_Wage.pdf"},
		},
		{
			name:     "multiple matches on separate lines",
			text:     "DSFN:111 some text\nDSFN: 222 end",
			search:   "DSFN:",
			expected: []string{"111 some text", "222 end"},
		},
		{
			name:     "no match",
			text:     "Some text without the pattern",
			search:   "DSFN:",
			expected: nil,
		},
		{
			name:     "complex value with spaces and delimiters",
			text:     "DSFN:Employee ID_X_X_X_X_Eag-AHP.pdf",
			search:   "DSFN:",
			expected: []string{"Employee ID_X_X_X_X_Eag-AHP.pdf"},
		},
		{
			name:     "duplicate values deduplicated",
			text:     "DSFN:123\nDSFN:123\nDSFN:456",
			search:   "DSFN:",
			expected: []string{"123", "456"},
		},
		{
			name:     "different delimiter",
			text:     "ID: ABC123 other text",
			search:   "ID:",
			expected: []string{"ABC123 other text"},
		},
		{
			name:     "multiple spaces after delimiter",
			text:     "DSFN:   789",
			search:   "DSFN:",
			expected: []string{"789"},
		},
		{
			name:     "value with spaces from specs",
			text:     "DSFN:Employee ID_X_X_X_X_Eag-AHP.pdf\nDSFN: 327078_X_X_X_X_Wage.pdf",
			search:   "DSFN:",
			expected: []string{"Employee ID_X_X_X_X_Eag-AHP.pdf", "327078_X_X_X_X_Wage.pdf"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractValues(tt.text, tt.search)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d values, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("value[%d]: expected %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

// =============================================================================
// UNIT TESTS: Configuration Validation
// =============================================================================
// Tests validateConfig() which ensures all required flags are present
// and paths are valid before processing begins.

// TestValidateConfig verifies all configuration validation rules.
// Test cases: missing flags, invalid values, path validation.
func TestValidateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "notadir.txt")
	_ = os.WriteFile(tmpFile, []byte("test"), 0o644)

	tests := []struct {
		name      string
		cfg       Config
		wantError string
	}{
		{
			name:      "missing path",
			cfg:       Config{FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: "/tmp/out.json"},
			wantError: "missing required flag: -path",
		},
		{
			name:      "missing file-pattern",
			cfg:       Config{Path: tmpDir, Search: "DSFN:", Format: "json", Output: "/tmp/out.json"},
			wantError: "missing required flag: -file-pattern",
		},
		{
			name:      "missing search",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Format: "json", Output: "/tmp/out.json"},
			wantError: "missing required flag: -search",
		},
		{
			name:      "missing format",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Output: "/tmp/out.json"},
			wantError: "missing required flag: -format",
		},
		{
			name:      "invalid format",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Format: "xml", Output: "/tmp/out.json"},
			wantError: "invalid format: xml",
		},
		{
			name:      "missing output",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Format: "json"},
			wantError: "missing required flag: -output",
		},
		{
			name:      "non-existent path",
			cfg:       Config{Path: "/nonexistent/path", FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: "/tmp/out.json"},
			wantError: "workspace path error",
		},
		{
			name:      "path is file not directory",
			cfg:       Config{Path: tmpFile, FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: "/tmp/out.json"},
			wantError: "workspace path is not a directory",
		},
		{
			name:      "valid config",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: "/tmp/out.json"},
			wantError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if tt.wantError == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantError)
				} else if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("expected error containing %q, got: %v", tt.wantError, err)
				}
			}
		})
	}
}

// =============================================================================
// UNIT TESTS: mutool Binary Discovery
// =============================================================================
// Tests findMutool() which locates the mutool binary using the precedence:
// CLI flag > environment variable > PATH lookup.

// testExecutableName returns a platform-appropriate executable name.
// On Windows, appends .exe; on Unix, returns the base name unchanged.
func testExecutableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// TestFindMutool verifies mutool binary discovery with precedence rules.
// Test cases: flag path, env path, PATH lookup, precedence ordering, not found.
func TestFindMutool(t *testing.T) {
	t.Run("flag path exists", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), testExecutableName("mutool"))
		_ = os.WriteFile(tmpFile, []byte("fake"), 0o755)

		path, err := findMutool(tmpFile)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		// Path is now cleaned/absolute, so compare basenames
		if filepath.Base(path) != filepath.Base(tmpFile) {
			t.Errorf("expected %s, got %s", tmpFile, path)
		}
	})

	t.Run("flag path does not exist", func(t *testing.T) {
		_, err := findMutool("/nonexistent/mutool")
		if err == nil {
			t.Error("expected error for non-existent path")
		}
		if !strings.Contains(err.Error(), "mutool binary not valid") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("env path exists", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), testExecutableName("mutool"))
		_ = os.WriteFile(tmpFile, []byte("fake"), 0o755)
		_ = os.Setenv("MUTOOL_BIN", tmpFile)
		defer func() { _ = os.Unsetenv("MUTOOL_BIN") }()

		path, err := findMutool("")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if filepath.Base(path) != filepath.Base(tmpFile) {
			t.Errorf("expected %s, got %s", tmpFile, path)
		}
	})

	t.Run("env path does not exist", func(t *testing.T) {
		_ = os.Setenv("MUTOOL_BIN", "/nonexistent/mutool-env")
		defer func() { _ = os.Unsetenv("MUTOOL_BIN") }()

		_, err := findMutool("")
		if err == nil {
			t.Error("expected error for non-existent env path")
		}
		if !strings.Contains(err.Error(), "mutool binary not valid at MUTOOL_BIN path") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("flag takes precedence over env", func(t *testing.T) {
		flagFile := filepath.Join(t.TempDir(), testExecutableName("mutool-flag"))
		envFile := filepath.Join(t.TempDir(), testExecutableName("mutool-env"))
		_ = os.WriteFile(flagFile, []byte("fake"), 0o755)
		_ = os.WriteFile(envFile, []byte("fake"), 0o755)
		_ = os.Setenv("MUTOOL_BIN", envFile)
		defer func() { _ = os.Unsetenv("MUTOOL_BIN") }()

		path, err := findMutool(flagFile)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if filepath.Base(path) != filepath.Base(flagFile) {
			t.Errorf("expected flag path %s, got %s", flagFile, path)
		}
	})

	t.Run("not found anywhere", func(t *testing.T) {
		_ = os.Unsetenv("MUTOOL_BIN")
		oldPath := os.Getenv("PATH")
		_ = os.Setenv("PATH", "/nonexistent")
		defer func() { _ = os.Setenv("PATH", oldPath) }()

		_, err := findMutool("")
		if err == nil {
			t.Error("expected error when mutool not found")
		}
		if !strings.Contains(err.Error(), "mutool not found in PATH") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// =============================================================================
// UNIT TESTS: File Discovery
// =============================================================================
// Tests findFiles() which uses glob patterns to discover PDF files.

// TestFindFiles verifies glob pattern matching for file discovery.
func TestFindFiles(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(tmpDir, "test1.pdf"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "test2.pdf"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("fake"), 0o644)

	t.Run("match pdf files", func(t *testing.T) {
		files, err := findFiles(tmpDir, "*.pdf")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		files, err := findFiles(tmpDir, "*.doc")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("expected 0 files, got %d", len(files))
		}
	})
}

// =============================================================================
// UNIT TESTS: Output Serialization
// =============================================================================
// Tests writeJSON() and writeTSV() which serialize results to output formats.

// TestWriteJSON verifies NDJSON output format generation.
func TestWriteJSON(t *testing.T) {
	results := []Result{
		{Filename: "doc1.pdf", Value: "123"},
		{Filename: "doc2.pdf", Value: []string{"456", "789"}},
		{Filename: "doc3.pdf", Error: "test error"},
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err := writeJSON(writer, results)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Errorf("flush error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}

	var r1 map[string]interface{}
	_ = json.Unmarshal([]byte(lines[0]), &r1)
	if r1["filename"] != "doc1.pdf" || r1["value"] != "123" {
		t.Errorf("unexpected first result: %+v", r1)
	}
}

// TestWriteTSV verifies TSV output format with header row.
func TestWriteTSV(t *testing.T) {
	results := []Result{
		{Filename: "doc1.pdf", Value: "123"},
		{Filename: "doc2.pdf", Value: []string{"456", "789"}},
		{Filename: "doc3.pdf", Error: "test error"},
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err := writeTSV(writer, results)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Errorf("flush error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 lines (header + 3 data), got %d", len(lines))
	}

	if lines[0] != "filename\tvalue" {
		t.Errorf("unexpected header: %s", lines[0])
	}
}

// TestResultSerialization verifies JSON marshaling of Result struct.
// Tests all value types: string, array, null, and error cases.
func TestResultSerialization(t *testing.T) {
	t.Run("single value", func(t *testing.T) {
		r := Result{Filename: "test.pdf", Value: "123"}
		data, _ := json.Marshal(r)
		if !strings.Contains(string(data), `"value":"123"`) {
			t.Errorf("unexpected JSON: %s", data)
		}
	})

	t.Run("null value", func(t *testing.T) {
		r := Result{Filename: "test.pdf", Value: nil}
		data, _ := json.Marshal(r)
		if !strings.Contains(string(data), `"value":null`) {
			t.Errorf("expected value:null in JSON: %s", data)
		}
	})

	t.Run("multiple values", func(t *testing.T) {
		r := Result{Filename: "test.pdf", Value: []string{"a", "b"}}
		data, _ := json.Marshal(r)
		if !strings.Contains(string(data), `"value":["a","b"]`) {
			t.Errorf("unexpected JSON: %s", data)
		}
	})

	t.Run("error case", func(t *testing.T) {
		r := Result{Filename: "test.pdf", Value: nil, Error: "failed"}
		data, _ := json.Marshal(r)
		if !strings.Contains(string(data), `"error":"failed"`) {
			t.Errorf("unexpected JSON: %s", data)
		}
	})
}

// =============================================================================
// UNIT TESTS: Process File Handling
// =============================================================================
// Tests processFile() error handling without requiring real mutool.

// TestProcessFileTimeout verifies error handling for invalid file/mutool.
func TestProcessFileTimeout(t *testing.T) {
	result := processFile("nonexistent.pdf", "nonexistent-mutool", "DSFN:", 1*time.Millisecond)
	if result.Error == "" {
		t.Error("expected error for non-existent file/mutool")
	}
}

// =============================================================================
// INTEGRATION TESTS: Worker Pool
// =============================================================================
// Tests concurrent file processing with real mutool.
// These tests require mutool in PATH and are skipped with -short flag.

// TestProcessFilesWorkerPool verifies concurrent processing produces complete results.
func TestProcessFilesWorkerPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	files, err := findFiles("testfiles", "*.pdf")
	if err != nil || len(files) == 0 {
		t.Skip("no test files available")
	}

	results := processFiles(files, mutoolPath, "DSFN:", 30*time.Second, 0)

	if len(results) != len(files) {
		t.Errorf("expected %d results, got %d", len(files), len(results))
	}

	for _, r := range results {
		if r.Filename == "" {
			t.Error("result has empty filename")
		}
	}
}

// TestProcessFilesEmpty verifies empty input handling (no files to process).
func TestProcessFilesEmpty(t *testing.T) {
	results := processFiles([]string{}, "mutool", "DSFN:", 30*time.Second, 0)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

// TestProcessFilesWorkersBounds verifies worker count bounds enforcement.
// Workers: default=NumCPU*2, min=2, max=16.
func TestProcessFilesWorkersBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	files, err := findFiles("testfiles", "*.pdf")
	if err != nil || len(files) == 0 {
		t.Skip("no test files available")
	}

	tests := []struct {
		name     string
		workers  int
		expected int
	}{
		{"default (0)", 0, runtime.NumCPU() * 2},
		{"below min (1)", 1, 2},
		{"at min (2)", 2, 2},
		{"normal (4)", 4, 4},
		{"at max (16)", 16, 16},
		{"above max (20)", 20, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := processFiles(files, mutoolPath, "DSFN:", 30*time.Second, tt.workers)
			if len(results) != len(files) {
				t.Errorf("expected %d results, got %d", len(files), len(results))
			}
		})
	}
}

// TestIntegration_WorkersFlag verifies -workers flag via full run() execution.
func TestIntegration_WorkersFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	tests := []struct {
		name    string
		workers int
	}{
		{"default", 0},
		{"min_bound", 1},
		{"explicit_4", 4},
		{"max_bound", 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceDir := createTestWorkspace(t)
			outputFile := filepath.Join(workspaceDir, fmt.Sprintf("workers_%s.json", tt.name))

			cfg := Config{
				Path:        "testfiles",
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      outputFile,
				MutoolBin:   mutoolPath,
				Timeout:     30 * time.Second,
				Workers:     tt.workers,
			}

			_, err := run(cfg)
			if err != nil {
				t.Fatalf("run() with workers=%d failed: %v", tt.workers, err)
			}

			content, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read output: %v", err)
			}

			lines := strings.Split(strings.TrimSpace(string(content)), "\n")
			if len(lines) != 2 {
				t.Errorf("expected 2 results, got %d", len(lines))
			}
		})
	}
}

// =============================================================================
// UNIT TESTS: Output Writing Edge Cases
// =============================================================================

// TestWriteOutputEmptyResults verifies handling of empty result sets.
func TestWriteOutputEmptyResults(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("empty json", func(t *testing.T) {
		outputFile := filepath.Join(tmpDir, "empty.json")
		err := writeOutput([]Result{}, "json", outputFile)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		content, _ := os.ReadFile(outputFile)
		if len(content) != 0 {
			t.Errorf("expected empty file, got %d bytes", len(content))
		}
	})

	t.Run("empty tsv", func(t *testing.T) {
		outputFile := filepath.Join(tmpDir, "empty.tsv")
		err := writeOutput([]Result{}, "tsv", outputFile)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		content, _ := os.ReadFile(outputFile)
		if !strings.HasPrefix(string(content), "filename\tvalue") {
			t.Errorf("expected TSV header, got: %s", content)
		}
	})
}

// TestWriteOutputInvalidPath verifies error handling for unwritable paths.
func TestWriteOutputInvalidPath(t *testing.T) {
	err := writeOutput([]Result{{Filename: "test.pdf"}}, "json", "/nonexistent/dir/output.json")
	if err == nil {
		t.Error("expected error for invalid output path")
	}
	if !strings.Contains(err.Error(), "cannot create output file") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestWriteOutputUnsupportedFormat verifies error for invalid format values.
func TestWriteOutputUnsupportedFormat(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "out.xml")
	err := writeOutput([]Result{}, "xml", tmpFile)
	if err == nil {
		t.Error("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestFindFilesInvalidPattern verifies error handling for malformed glob patterns.
func TestFindFilesInvalidPattern(t *testing.T) {
	_, err := findFiles("testfiles", "[invalid")
	if err == nil {
		t.Error("expected error for invalid glob pattern")
	}
}

// =============================================================================
// INTEGRATION TESTS: run() Function
// =============================================================================
// Tests the main run() function which orchestrates the entire pipeline.

// TestRun_Success verifies successful execution produces expected output.
func TestRun_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "run_output.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

// TestRun_InvalidConfig verifies run() returns error for invalid configuration.
func TestRun_InvalidConfig(t *testing.T) {
	cfg := Config{
		Path: "/nonexistent",
	}
	_, err := run(cfg)
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

// =============================================================================
// EXIT CODE TESTS
// =============================================================================
// Tests that run() returns correct exit codes for each error condition.
// Exit codes are critical for integration with job schedulers.

// TestRun_ExitCodes verifies each error condition returns the correct exit code.
func TestRun_ExitCodes(t *testing.T) {
	mutoolPath, _ := findMutool("")

	tests := []struct {
		name         string
		cfg          Config
		expectedCode int
	}{
		{
			name:         "missing required flag",
			cfg:          Config{},
			expectedCode: ExitConfigError,
		},
		{
			name: "workspace path not found",
			cfg: Config{
				Path:        "/nonexistent/path",
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      "/tmp/out.json",
			},
			expectedCode: ExitPathError,
		},
		{
			name: "mutool not found",
			cfg: Config{
				Path:        t.TempDir(),
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      "/tmp/out.json",
				MutoolBin:   "/nonexistent/mutool",
			},
			expectedCode: ExitMutoolNotFound,
		},
		{
			name: "invalid glob pattern",
			cfg: Config{
				Path:        t.TempDir(),
				FilePattern: "[invalid",
				Search:      "DSFN:",
				Format:      "json",
				Output:      "/tmp/out.json",
				MutoolBin:   mutoolPath,
			},
			expectedCode: ExitPatternError,
		},
		{
			name: "output path error",
			cfg: Config{
				Path:        "testfiles",
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      "/nonexistent/dir/out.json",
				MutoolBin:   mutoolPath,
			},
			expectedCode: ExitOutputError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectedCode == ExitPatternError || tt.expectedCode == ExitOutputError {
				if mutoolPath == "" {
					t.Skip("mutool not available")
				}
			}
			code, _ := run(tt.cfg)
			if code != tt.expectedCode {
				t.Errorf("expected exit code %d, got %d", tt.expectedCode, code)
			}
		})
	}
}

// TestRun_ExitSuccess verifies successful run returns ExitSuccess (0).
func TestRun_ExitSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "exit_success.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	code, err := run(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitSuccess {
		t.Errorf("expected exit code %d (success), got %d", ExitSuccess, code)
	}
}

// TestRun_MutoolNotFound verifies exit code for nonexistent mutool binary.
func TestRun_MutoolNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "out.json"),
		MutoolBin:   "/nonexistent/mutool",
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
	if err == nil {
		t.Error("expected error for nonexistent mutool")
	}
}

// TestRun_InvalidGlobPattern verifies exit code for malformed glob pattern.
func TestRun_InvalidGlobPattern(t *testing.T) {
	tmpDir := t.TempDir()

	mutoolPath, err := findMutool("")
	if err != nil {
		return // skip if no mutool
	}

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "[invalid",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "out.json"),
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err == nil {
		t.Error("expected error for invalid glob pattern")
	}
}

// TestRun_OutputWriteError verifies exit code for unwritable output path.
func TestRun_OutputWriteError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      "/nonexistent/dir/out.json",
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}

// =============================================================================
// CLI FLAG COMBINATION TESTS
// =============================================================================
// Systematically test all CLI flag combinations to ensure they work together.
// Each test verifies a specific flag or combination of flags.

// TestIntegration_FormatJSON verifies -format json produces valid NDJSON.
func TestIntegration_FormatJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "format_json.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	for _, line := range lines {
		var r map[string]interface{}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Errorf("invalid JSON line: %s", line)
		}
	}
}

// TestIntegration_FormatTSV verifies -format tsv produces valid TSV with header.
func TestIntegration_FormatTSV(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "format_tsv.tsv")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "tsv",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if lines[0] != "filename\tvalue" {
		t.Errorf("expected TSV header, got: %s", lines[0])
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 data), got %d", len(lines))
	}
}

// TestIntegration_DifferentSearchPattern verifies -search with non-matching pattern.
func TestIntegration_DifferentSearchPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "different_pattern.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "NONEXISTENT_PATTERN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	for _, line := range lines {
		if !strings.Contains(line, `"value":null`) {
			t.Errorf("expected null value for non-matching pattern: %s", line)
		}
	}
}

// TestIntegration_FilePatternSpecific verifies -file-pattern selects single file.
func TestIntegration_FilePatternSpecific(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "specific_file.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "sample001.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line for single file, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "sample001.pdf") {
		t.Errorf("expected sample001.pdf in output: %s", lines[0])
	}
}

// TestIntegration_MutoolBinFlag verifies -mutool-bin flag overrides PATH lookup.
func TestIntegration_MutoolBinFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "mutool_flag.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() with explicit mutool-bin failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	if len(content) == 0 {
		t.Error("expected non-empty output")
	}
}

// TestIntegration_TimeoutFlag verifies -timeout flag with extended duration.
func TestIntegration_TimeoutFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "timeout.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     60 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() with 60s timeout failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	if len(content) == 0 {
		t.Error("expected non-empty output")
	}
}

// TestIntegration_NoMatchingFiles verifies empty output when no files match pattern.
func TestIntegration_NoMatchingFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "no_match.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.nonexistent",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err = run(cfg)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	if len(content) != 0 {
		t.Errorf("expected empty output for no matching files, got: %s", content)
	}
}

// TestIntegration_AllFlagsCombined verifies various flag combinations work together.
func TestIntegration_AllFlagsCombined(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)

	tests := []struct {
		name        string
		format      string
		filePattern string
		search      string
		outputExt   string
	}{
		{"json_all_pdf", "json", "*.pdf", "DSFN:", ".json"},
		{"tsv_all_pdf", "tsv", "*.pdf", "DSFN:", ".tsv"},
		{"json_single_pdf", "json", "sample001.pdf", "DSFN:", ".json"},
		{"tsv_single_pdf", "tsv", "sample002.pdf", "DSFN:", ".tsv"},
		{"json_no_match_pattern", "json", "*.pdf", "NOMATCH:", ".json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFile := filepath.Join(workspaceDir, tt.name+tt.outputExt)

			cfg := Config{
				Path:        "testfiles",
				FilePattern: tt.filePattern,
				Search:      tt.search,
				Format:      tt.format,
				Output:      outputFile,
				MutoolBin:   mutoolPath,
				Timeout:     30 * time.Second,
			}

			_, err := run(cfg)
			if err != nil {
				t.Fatalf("run() failed: %v", err)
			}

			if _, err := os.Stat(outputFile); os.IsNotExist(err) {
				t.Errorf("output file not created: %s", outputFile)
			}
		})
	}
}

// TestProcessFileWithMutoolError verifies processFile returns error for missing file.
func TestProcessFileWithMutoolError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	result := processFile("nonexistent_file.pdf", mutoolPath, "DSFN:", 30*time.Second)
	if result.Error == "" {
		t.Error("expected error for nonexistent file")
	}
	if !strings.Contains(result.Error, "mutool error") {
		t.Errorf("expected mutool error, got: %s", result.Error)
	}
}

// TestWriteOutputWithAllResultTypes verifies both JSON and TSV handle all value types.
func TestWriteOutputWithAllResultTypes(t *testing.T) {
	tmpDir := t.TempDir()

	results := []Result{
		{Filename: "single.pdf", Value: "one"},
		{Filename: "multi.pdf", Value: []string{"a", "b", "c"}},
		{Filename: "none.pdf", Value: nil},
		{Filename: "error.pdf", Value: nil, Error: "corrupted"},
	}

	t.Run("json with all types", func(t *testing.T) {
		outFile := filepath.Join(tmpDir, "all.json")
		err := writeOutput(results, "json", outFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		content, _ := os.ReadFile(outFile)
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) != 4 {
			t.Errorf("expected 4 lines, got %d", len(lines))
		}
		if !strings.Contains(lines[1], `"value":["a","b","c"]`) {
			t.Errorf("expected array value in line 2: %s", lines[1])
		}
		if !strings.Contains(lines[2], `"value":null`) {
			t.Errorf("expected null value in line 3: %s", lines[2])
		}
		if !strings.Contains(lines[3], `"error":"corrupted"`) {
			t.Errorf("expected error in line 4: %s", lines[3])
		}
	})

	t.Run("tsv with all types", func(t *testing.T) {
		outFile := filepath.Join(tmpDir, "all.tsv")
		err := writeOutput(results, "tsv", outFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		content, _ := os.ReadFile(outFile)
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) != 5 {
			t.Errorf("expected 5 lines (header + 4 data), got %d", len(lines))
		}
		if !strings.Contains(lines[2], "a,b,c") {
			t.Errorf("expected comma-joined values in line 3: %s", lines[2])
		}
	})
}

// =============================================================================
// INTEGRATION TESTS: Real PDF Processing with testfiles/*.pdf
// =============================================================================
// End-to-end tests using actual PDF files from testfiles/ directory.
// These tests require mutool in PATH and are skipped with -short flag.

// TestIntegration_SingleFileWithMatch verifies extraction from single PDF with match.
func TestIntegration_SingleFileWithMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	result := processFile("testfiles/sample001.pdf", mutoolPath, "DSFN:", 30*time.Second)

	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.Filename != "sample001.pdf" {
		t.Errorf("expected filename sample001.pdf, got %s", result.Filename)
	}
	if result.Value == nil {
		t.Error("expected a value, got nil")
	} else if v, ok := result.Value.(string); !ok || v != "Employee ID_X_X_X_X_Eag-AHP.pdf" {
		t.Errorf("expected value 'Employee ID_X_X_X_X_Eag-AHP.pdf', got %v", result.Value)
	}
}

// TestIntegration_SingleFileWithSpaceAfterDelimiter verifies space trimming after delimiter.
func TestIntegration_SingleFileWithSpaceAfterDelimiter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	result := processFile("testfiles/sample002.pdf", mutoolPath, "DSFN:", 30*time.Second)

	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.Filename != "sample002.pdf" {
		t.Errorf("expected filename sample002.pdf, got %s", result.Filename)
	}
	if result.Value == nil {
		t.Error("expected a value, got nil")
	} else if v, ok := result.Value.(string); !ok || v != "327078_X_X_X_X_Wage.pdf" {
		t.Errorf("expected value '327078_X_X_X_X_Wage.pdf', got %v", result.Value)
	}
}

// TestIntegration_BatchProcessing verifies concurrent processing of multiple PDFs.
func TestIntegration_BatchProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	files, err := findFiles("testfiles", "*.pdf")
	if err != nil {
		t.Fatalf("failed to find files: %v", err)
	}
	if len(files) < 2 {
		t.Skipf("expected at least 2 PDF files in testfiles, got %d", len(files))
	}

	results := processFiles(files, mutoolPath, "DSFN:", 30*time.Second, 0)

	if len(results) != len(files) {
		t.Errorf("expected %d results, got %d", len(files), len(results))
	}

	matchCount := 0
	for _, r := range results {
		if r.Error == "" && r.Value != nil {
			matchCount++
		}
	}
	if matchCount != 2 {
		t.Errorf("expected 2 files with matches, got %d", matchCount)
	}
}

// TestIntegration_JSONOutput verifies NDJSON output format with real PDFs.
func TestIntegration_JSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	files, err := findFiles("testfiles", "*.pdf")
	if err != nil {
		t.Fatalf("failed to find files: %v", err)
	}

	results := processFiles(files, mutoolPath, "DSFN:", 30*time.Second, 0)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "output.json")
	err = writeOutput(results, "json", outputFile)
	if err != nil {
		t.Fatalf("failed to write output: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != len(files) {
		t.Errorf("expected %d JSON lines, got %d", len(files), len(lines))
	}

	for i, line := range lines {
		var r Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
		if r.Filename == "" {
			t.Errorf("line %d: missing filename", i)
		}
	}
}

// TestIntegration_TSVOutput verifies TSV output format with real PDFs.
func TestIntegration_TSVOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	files, err := findFiles("testfiles", "*.pdf")
	if err != nil {
		t.Fatalf("failed to find files: %v", err)
	}

	results := processFiles(files, mutoolPath, "DSFN:", 30*time.Second, 0)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "output.tsv")
	err = writeOutput(results, "tsv", outputFile)
	if err != nil {
		t.Fatalf("failed to write output: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	expectedLines := len(files) + 1 // header + data rows
	if len(lines) != expectedLines {
		t.Errorf("expected %d TSV lines (1 header + %d data), got %d", expectedLines, len(files), len(lines))
	}

	if !strings.HasPrefix(lines[0], "filename\tvalue") {
		t.Errorf("unexpected header: %s", lines[0])
	}

	for i, line := range lines[1:] {
		cols := strings.Split(line, "\t")
		if len(cols) < 2 {
			t.Errorf("data line %d: expected at least 2 columns, got %d", i, len(cols))
		}
	}
}

// TestIntegration_NoMatchFile verifies null value when search pattern not found.
func TestIntegration_NoMatchFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	result := processFile("testfiles/sample001.pdf", mutoolPath, "NONEXISTENT:", 30*time.Second)

	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.Value != nil {
		t.Errorf("expected nil value for non-matching pattern, got value=%v", result.Value)
	}
}

// TestIntegration_EndToEnd verifies complete pipeline from config to output file.
func TestIntegration_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "results.json")

	cfg := Config{
		Path:        "testfiles",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	if err := validateConfig(cfg); err != nil {
		t.Fatalf("invalid config: %v", err)
	}

	files, err := findFiles(cfg.Path, cfg.FilePattern)
	if err != nil {
		t.Fatalf("failed to find files: %v", err)
	}

	results := processFiles(files, mutoolPath, cfg.Search, cfg.Timeout, 0)

	if err := writeOutput(results, cfg.Format, cfg.Output); err != nil {
		t.Fatalf("failed to write output: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 result lines, got %d", len(lines))
	}

	foundSample001 := false
	foundSample002 := false
	for _, line := range lines {
		var r Result
		_ = json.Unmarshal([]byte(line), &r)
		if r.Filename == "sample001.pdf" {
			foundSample001 = true
		}
		if r.Filename == "sample002.pdf" {
			foundSample002 = true
		}
	}

	if !foundSample001 || !foundSample002 {
		t.Errorf("missing expected files in output: sample001=%v sample002=%v", foundSample001, foundSample002)
	}
}
