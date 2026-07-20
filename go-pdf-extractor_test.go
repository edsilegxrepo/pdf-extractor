// Package main provides tests for go-pdf-extractor.
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
	"flag"
	"fmt"
	"os"
	"os/exec"
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
		{
			name:     "CRLF line endings",
			text:     "DSFN:value1\r\nDSFN:value2\r\n",
			search:   "DSFN:",
			expected: []string{"value1", "value2"},
		},
		{
			name:     "CR only line endings",
			text:     "DSFN:value1\rDSFN:value2\r",
			search:   "DSFN:",
			expected: []string{"value1", "value2"},
		},
		{
			name:     "mixed line endings",
			text:     "DSFN:value1\r\nDSFN:value2\nDSFN:value3\r",
			search:   "DSFN:",
			expected: []string{"value1", "value2", "value3"},
		},
		{
			name:     "UTF-8/Unicode values",
			text:     "DSFN:Müller-AHP.pdf\nDSFN: 測試_123.pdf\nDSFN: 😊_Smile.pdf",
			search:   "DSFN:",
			expected: []string{"Müller-AHP.pdf", "測試_123.pdf", "😊_Smile.pdf"},
		},
		{
			name:     "nested delimiter in value",
			text:     "DSFN:DSFN:nested_value",
			search:   "DSFN:",
			expected: []string{"DSFN:nested_value"},
		},
		{
			name:     "regex characters in delimiter are matched literally",
			text:     "DSFN[123]: value\n*DSFN: other",
			search:   "DSFN[123]:",
			expected: []string{"value"},
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

	// Use temp dir for output paths (satisfies depth requirements)
	outputPath := filepath.Join(tmpDir, "out.json")

	tests := []struct {
		name      string
		cfg       Config
		wantError string
	}{
		{
			name:      "missing path",
			cfg:       Config{FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: outputPath},
			wantError: "missing required flag: -path",
		},
		{
			name:      "missing file-pattern",
			cfg:       Config{Path: tmpDir, Search: "DSFN:", Format: "json", Output: outputPath},
			wantError: "missing required flag: -file-pattern",
		},
		{
			name:      "missing search",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Format: "json", Output: outputPath},
			wantError: "missing required flag: -search",
		},
		{
			name:      "missing format",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Output: outputPath},
			wantError: "missing required flag: -format",
		},
		{
			name:      "invalid format",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Format: "xml", Output: outputPath},
			wantError: "invalid format: xml",
		},
		{
			name:      "missing output",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Format: "json"},
			wantError: "missing required flag: -output",
		},
		{
			name:      "non-existent path",
			cfg:       Config{Path: filepath.Join(tmpDir, "nonexistent", "path"), FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: outputPath},
			wantError: "workspace path error",
		},
		{
			name:      "path is file not directory",
			cfg:       Config{Path: tmpFile, FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: outputPath},
			wantError: "workspace path is not a directory",
		},
		{
			name:      "valid config",
			cfg:       Config{Path: tmpDir, FilePattern: "*.pdf", Search: "DSFN:", Format: "json", Output: outputPath},
			wantError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.cfg)
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

// testfilesPath returns the absolute path to the testfiles directory.
func testfilesPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("testfiles")
	if err != nil {
		t.Fatalf("failed to get absolute path for testfiles: %v", err)
	}
	return abs
}

// TestFindMutool verifies mutool binary discovery with precedence rules.
// Test cases: flag path, env path, PATH lookup, precedence ordering, not found.
func TestFindMutool(t *testing.T) {
	origMutoolBin := os.Getenv("MUTOOL_BIN")
	t.Cleanup(func() {
		if origMutoolBin != "" {
			_ = os.Setenv("MUTOOL_BIN", origMutoolBin)
		} else {
			_ = os.Unsetenv("MUTOOL_BIN")
		}
	})

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
		// Use a valid-looking path that doesn't exist
		nonExistent := filepath.Join(t.TempDir(), "subdir", "mutool")
		_, err := findMutool(nonExistent)
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
		// Use a valid-looking path that doesn't exist
		nonExistent := filepath.Join(t.TempDir(), "subdir", "mutool-env")
		_ = os.Setenv("MUTOOL_BIN", nonExistent)
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
		oldMutoolBin := os.Getenv("MUTOOL_BIN")
		_ = os.Unsetenv("MUTOOL_BIN")
		defer func() {
			if oldMutoolBin != "" {
				_ = os.Setenv("MUTOOL_BIN", oldMutoolBin)
			}
		}()
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

// TestFindFiles_PathTraversal verifies path traversal patterns are rejected.
func TestFindFiles_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		pattern string
		wantErr string
	}{
		{
			name:    "dotdot in pattern",
			pattern: "../*.pdf",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "dotdot in middle",
			pattern: "subdir/../../../etc/passwd",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "encoded dotdot",
			pattern: "..\\*.pdf",
			wantErr: "path traversal not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findFiles(tmpDir, tt.pattern)
			if err == nil {
				t.Error("expected error for path traversal pattern, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
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

// TestWriteTSV_SpecialCharacters verifies TSV sanitizes tabs/newlines in values.
func TestWriteTSV_SpecialCharacters(t *testing.T) {
	results := []Result{
		{Filename: "doc\twith\ttabs.pdf", Value: "value\twith\ttab"},
		{Filename: "doc\nwith\nnewlines.pdf", Value: "value\nwith\nnewline"},
		{Filename: "doc\r\nwith\r\ncrlf.pdf", Value: "value\r\nwith\r\ncrlf"},
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err := writeTSV(writer, results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	output := buf.String()

	// Verify no raw tabs in data (only field separators)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			t.Errorf("line %d should have exactly 2 tab-separated fields, got %d: %q", i, len(fields), line)
		}
		// Verify no embedded tabs/newlines in field values (they should be replaced with spaces)
		for j, field := range fields {
			if strings.ContainsAny(field, "\t\n\r") {
				t.Errorf("line %d field %d contains raw tab/newline: %q", i, j, field)
			}
		}
	}
}

// TestSanitizeTSV verifies special character handling.
func TestSanitizeTSV(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"with\ttab", "with tab"},
		{"with\nnewline", "with newline"},
		{"with\rcarriage", "with carriage"},
		{"with\r\ncrlf", "with  crlf"},
		{"mixed\t\n\rall", "mixed   all"},
	}

	for _, tt := range tests {
		result := sanitizeTSV(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeTSV(%q) = %q, want %q", tt.input, result, tt.expected)
		}
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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

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
				Path:        testfilesPath(t),
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
	// Use a valid-looking path with non-existent parent directory
	invalidPath := filepath.Join(t.TempDir(), "nonexistent", "subdir", "output.json")
	err := writeOutput([]Result{{Filename: "test.pdf"}}, "json", invalidPath)
	if err == nil {
		t.Error("expected error for invalid output path")
	}
	if !strings.Contains(err.Error(), "cannot create temp file") {
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

	mutoolPath := requireMutool(t)

	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "run_output.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
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
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "out.json")

	// Get absolute path to testfiles
	testfilesAbs, _ := filepath.Abs("testfiles")

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
				Path:        filepath.Join(tmpDir, "nonexistent", "path"),
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      outputPath,
			},
			expectedCode: ExitPathError,
		},
		{
			name: "mutool not found",
			cfg: Config{
				Path:        tmpDir,
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      outputPath,
				MutoolBin:   filepath.Join(tmpDir, "nonexistent", "mutool"),
			},
			expectedCode: ExitMutoolNotFound,
		},
		{
			name: "invalid glob pattern",
			cfg: Config{
				Path:        tmpDir,
				FilePattern: "[invalid",
				Search:      "DSFN:",
				Format:      "json",
				Output:      outputPath,
				MutoolBin:   mutoolPath,
			},
			expectedCode: ExitPatternError,
		},
		{
			name: "output path error",
			cfg: Config{
				Path:        testfilesAbs,
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      filepath.Join(tmpDir, "nonexistent", "subdir", "out.json"),
				MutoolBin:   mutoolPath,
			},
			expectedCode: ExitOutputError,
		},
		{
			name: "no files found",
			cfg: Config{
				Path:        tmpDir,
				FilePattern: "*.pdf",
				Search:      "DSFN:",
				Format:      "json",
				Output:      outputPath,
				MutoolBin:   mutoolPath,
			},
			expectedCode: ExitNoFilesFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectedCode == ExitPatternError || tt.expectedCode == ExitOutputError || tt.expectedCode == ExitNoFilesFound {
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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "exit_success.json")

	cfg := Config{
		Path:        testfilesPath(t),
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

	mutoolPath := requireMutool(t)

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "[invalid",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "out.json"),
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
	if err == nil {
		t.Error("expected error for invalid glob pattern")
	}
}

// TestRun_OutputWriteError verifies exit code for unwritable output path.
func TestRun_OutputWriteError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      "/nonexistent/dir/out.json",
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "format_json.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "format_tsv.tsv")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "tsv",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "different_pattern.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "NONEXISTENT_PATTERN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "specific_file.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "sample001.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "mutool_flag.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	_, err := run(cfg)
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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "timeout.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     60 * time.Second,
	}

	_, err := run(cfg)
	if err != nil {
		t.Fatalf("run() with 60s timeout failed: %v", err)
	}

	content, _ := os.ReadFile(outputFile)
	if len(content) == 0 {
		t.Error("expected non-empty output")
	}
}

// TestIntegration_NoMatchingFiles verifies ExitNoFilesFound when no files match pattern.
func TestIntegration_NoMatchingFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "no_match.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.nonexistent",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	exitCode, err := run(cfg)
	if exitCode != ExitNoFilesFound {
		t.Errorf("expected exit code %d (ExitNoFilesFound), got %d", ExitNoFilesFound, exitCode)
	}
	if err == nil {
		t.Error("expected error for no matching files, got nil")
	}
}

// TestIntegration_AllFlagsCombined verifies various flag combinations work together.
func TestIntegration_AllFlagsCombined(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

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
				Path:        testfilesPath(t),
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

	mutoolPath := requireMutool(t)

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
		if !strings.Contains(lines[2], "a|b|c") {
			t.Errorf("expected pipe-joined values in line 3: %s", lines[2])
		}
	})

	// Test pipe escaping in multi-values
	t.Run("tsv_pipe_escaping", func(t *testing.T) {
		tmpDir := t.TempDir()
		results := []Result{
			{Filename: "test.pdf", Value: []string{"val|with|pipes", "normal"}},
		}
		outFile := filepath.Join(tmpDir, "escaped.tsv")
		err := writeOutput(results, "tsv", outFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		content, _ := os.ReadFile(outFile)
		// Pipes in values should be escaped as \|
		if !strings.Contains(string(content), `val\|with\|pipes|normal`) {
			t.Errorf("expected escaped pipes in output: %s", string(content))
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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

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

	mutoolPath := requireMutool(t)

	workspaceDir := createTestWorkspace(t)
	outputFile := filepath.Join(workspaceDir, "results.json")

	cfg := Config{
		Path:        testfilesPath(t),
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     30 * time.Second,
	}

	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("invalid config: %v", err)
	}

	files, err := findFiles(cfg.cleanPath, cfg.FilePattern)
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

// =============================================================================
// DETECT MODE TESTS
// =============================================================================

// TestRunDetect_Success tests successful prerequisite detection.
func TestRunDetect_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.json")

	// Create a test PDF file with search pattern
	testPDF := filepath.Join(tmpDir, "test.pdf")
	createTestPDFWithContent(t, testPDF, "DSFN:TestValue")

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      outputFile,
		MutoolBin:   mutoolPath,
		Timeout:     defaultTimeout,
		Detect:      true,
	}

	code, err := runDetect(cfg)
	if err != nil {
		t.Errorf("runDetect failed: %v", err)
	}
	if code != ExitSuccess {
		t.Errorf("expected exit code %d, got %d", ExitSuccess, code)
	}
}

// TestRunDetect_PathNotReadable tests detection with non-existent path.
func TestRunDetect_PathNotReadable(t *testing.T) {
	cfg := Config{
		Path:        "/nonexistent/path/that/does/not/exist",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      "/tmp/out.json",
	}

	code, err := runDetect(cfg)
	if code != ExitPathError {
		t.Errorf("expected exit code %d, got %d", ExitPathError, code)
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestRunDetect_NoFilesFound tests detection when no files match pattern.
func TestRunDetect_NoFilesFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "out.json"),
	}

	code, err := runDetect(cfg)
	if code != ExitNoFilesFound {
		t.Errorf("expected exit code %d, got %d", ExitNoFilesFound, code)
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestRunDetect_MutoolNotFound tests detection when mutool is not found.
func TestRunDetect_MutoolNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a dummy file to pass file pattern check
	dummyFile := filepath.Join(tmpDir, "test.pdf")
	if err := os.WriteFile(dummyFile, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "out.json"),
		MutoolBin:   "/nonexistent/mutool",
	}

	code, err := runDetect(cfg)
	if code != ExitMutoolNotFound {
		t.Errorf("expected exit code %d, got %d", ExitMutoolNotFound, code)
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestRunDetect_OutputNotWritable tests detection when output is not writable.
func TestRunDetect_OutputNotWritable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	tmpDir := t.TempDir()

	// Create a test PDF with search pattern
	testPDF := filepath.Join(tmpDir, "test.pdf")
	createTestPDFWithContent(t, testPDF, "DSFN:TestValue")

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "nonexistent", "subdir", "out.json"),
		MutoolBin:   mutoolPath,
		Timeout:     defaultTimeout,
	}

	code, err := runDetect(cfg)
	if code != ExitOutputError {
		t.Errorf("expected exit code %d, got %d", ExitOutputError, code)
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestRunDetect_SearchNotFound tests detection when search pattern not in files.
func TestRunDetect_SearchNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	tmpDir := t.TempDir()

	// Create a test PDF without the search pattern
	testPDF := filepath.Join(tmpDir, "test.pdf")
	createTestPDFWithContent(t, testPDF, "SomeOtherContent")

	cfg := Config{
		Path:        tmpDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "out.json"),
		MutoolBin:   mutoolPath,
		Timeout:     defaultTimeout,
	}

	code, err := runDetect(cfg)
	if code != ExitSearchNotFound {
		t.Errorf("expected exit code %d, got %d", ExitSearchNotFound, code)
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestTestMutoolExecution tests the mutool execution validator.
func TestTestMutoolExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	err := testMutoolExecution(mutoolPath)
	if err != nil {
		t.Errorf("testMutoolExecution failed for valid mutool: %v", err)
	}
}

// TestTestMutoolExecution_Invalid tests execution with invalid binary.
func TestTestMutoolExecution_Invalid(t *testing.T) {
	err := testMutoolExecution("/nonexistent/binary")
	if err == nil {
		t.Error("expected error for nonexistent binary, got nil")
	}
}

// TestDetectSearchPattern tests the search pattern detection.
func TestDetectSearchPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	tmpDir := t.TempDir()

	// Create test PDFs
	pdf1 := filepath.Join(tmpDir, "test1.pdf")
	pdf2 := filepath.Join(tmpDir, "test2.pdf")
	createTestPDFWithContent(t, pdf1, "NoMatch")
	createTestPDFWithContent(t, pdf2, "DSFN:FoundIt")

	files := []string{pdf1, pdf2}

	found, checkedCount, err := detectSearchPattern(files, mutoolPath, "DSFN:", defaultTimeout)
	if err != nil {
		t.Errorf("detectSearchPattern failed: %v", err)
	}
	if !found {
		t.Error("expected pattern to be found")
	}
	if checkedCount > 2 {
		t.Errorf("expected to check at most 2 files, checked %d", checkedCount)
	}
}

// TestDetectSearchPattern_NotFound tests when pattern is not in any file.
func TestDetectSearchPattern_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	tmpDir := t.TempDir()

	// Create test PDF without pattern
	pdf1 := filepath.Join(tmpDir, "test1.pdf")
	createTestPDFWithContent(t, pdf1, "NoMatchHere")

	files := []string{pdf1}

	found, checkedCount, err := detectSearchPattern(files, mutoolPath, "DSFN:", defaultTimeout)
	if err != nil {
		t.Errorf("detectSearchPattern failed: %v", err)
	}
	if found {
		t.Error("expected pattern not to be found")
	}
	if checkedCount != 1 {
		t.Errorf("expected to check 1 file, checked %d", checkedCount)
	}
}

// TestTestOutputWritable tests the output writeability check.
func TestTestOutputWritable(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test_output.json")

	err := testOutputWritable(outputPath)
	if err != nil {
		t.Errorf("testOutputWritable failed for valid path: %v", err)
	}

	// Verify test file was cleaned up
	if _, err := os.Stat(outputPath + ".detect-test"); err == nil {
		t.Error("test file was not cleaned up")
	}
}

// TestTestOutputWritable_NotWritable tests with non-writable path.
func TestTestOutputWritable_NotWritable(t *testing.T) {
	err := testOutputWritable("/nonexistent/dir/output.json")
	if err == nil {
		t.Error("expected error for non-writable path, got nil")
	}
}

// TestTestOutputWritable_InvalidPath tests with invalid path.
func TestTestOutputWritable_InvalidPath(t *testing.T) {
	err := testOutputWritable("")
	if err == nil {
		t.Error("expected error for empty path, got nil")
	}
}

// TestRunDetect_ExitCodeInErrorMessage verifies error messages include exit codes.
func TestRunDetect_ExitCodeInErrorMessage(t *testing.T) {
	cfg := Config{
		Path:        "/nonexistent/path",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      "/tmp/out.json",
	}

	_, err := runDetect(cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "[exit") {
		t.Errorf("error message should contain exit code, got: %s", errMsg)
	}
}

// TestSanitizePathExt tests the unified path sanitization logic for both files and executables.
func TestSanitizePathExt(t *testing.T) {
	t.Run("common checks", func(t *testing.T) {
		for _, allowSystemDirs := range []bool{false, true} {
			// Null byte rejection
			if _, err := sanitizePathExt("/data/path\x00with\x00nulls", allowSystemDirs); err == nil {
				t.Errorf("expected error for path with null bytes (allowSystemDirs=%v), got nil", allowSystemDirs)
			}

			// Empty path rejection
			if _, err := sanitizePathExt("", allowSystemDirs); err == nil {
				t.Errorf("expected error for empty path (allowSystemDirs=%v), got nil", allowSystemDirs)
			}

			// Traversal rejection
			if _, err := sanitizePathExt("/data/workspace/../../../etc/passwd", allowSystemDirs); err == nil {
				t.Errorf("expected error for path traversal (allowSystemDirs=%v), got nil", allowSystemDirs)
			}

			// Relative path rejection
			if _, err := sanitizePathExt("relative/path/file.txt", allowSystemDirs); err == nil {
				t.Errorf("expected error for relative path (allowSystemDirs=%v), got nil", allowSystemDirs)
			}

			// Control characters rejection
			if _, err := sanitizePathExt("/data/path\twith\ttabs", allowSystemDirs); err == nil {
				t.Errorf("expected error for control characters (allowSystemDirs=%v), got nil", allowSystemDirs)
			}
		}
	})

	t.Run("system directory allowance", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping Unix-specific system directory tests on Windows")
		}

		systemPaths := []string{
			"/usr/bin/mutool",
			"/bin/mutool",
		}

		for _, path := range systemPaths {
			// Should fail when allowSystemDirs is false
			if _, err := sanitizePathExt(path, false); err == nil {
				t.Errorf("expected error for system directory path %q when allowSystemDirs=false, got nil", path)
			}

			// Should succeed when allowSystemDirs is true
			if _, err := sanitizePathExt(path, true); err != nil {
				t.Errorf("unexpected error for system directory path %q when allowSystemDirs=true: %v", path, err)
			}
		}
	})
}

// TestSanitizePath_Security tests path security validation for sanitizePath (which blocks system directories).
func TestSanitizePath_Security(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "path traversal",
			path:    "/data/workspace/../../../etc/passwd",
			wantErr: "path traversal",
		},
		{
			name:    "relative path",
			path:    "relative/path/file.txt",
			wantErr: "must be absolute",
		},
		{
			name:    "control characters",
			path:    "/data/path\twith\ttabs",
			wantErr: "invalid characters",
		},
	}

	// Add platform-specific tests
	if runtime.GOOS == "windows" {
		tests = append(tests, []struct {
			name    string
			path    string
			wantErr string
		}{
			{
				name:    "windows root",
				path:    "C:\\",
				wantErr: "files in root directory",
			},
			{
				name:    "windows file in root",
				path:    "C:\\file.txt",
				wantErr: "files in root directory",
			},
			{
				name:    "UNC admin share C$",
				path:    "\\\\server\\C$\\Windows\\file.txt",
				wantErr: "administrative shares",
			},
			{
				name:    "UNC admin share ADMIN$",
				path:    "\\\\server\\ADMIN$\\system\\file.txt",
				wantErr: "administrative shares",
			},
			{
				name:    "UNC admin share lowercase",
				path:    "\\\\server\\d$\\data\\file.txt",
				wantErr: "administrative shares",
			},
		}...)
	} else {
		tests = append(tests, []struct {
			name    string
			path    string
			wantErr string
		}{
			{
				name:    "unix root",
				path:    "/",
				wantErr: "root paths",
			},
			{
				name:    "unix file in root",
				path:    "/file.txt",
				wantErr: "files in root directory",
			},
			{
				name:    "etc directory",
				path:    "/etc/passwd",
				wantErr: "system directory",
			},
			{
				name:    "usr directory",
				path:    "/usr/local/bin",
				wantErr: "system directory",
			},
			{
				name:    "bin directory",
				path:    "/bin/sh",
				wantErr: "system directory",
			},
		}...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizePath(tt.path)
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidatePathSecurityOS checks path security rules for both Windows and Unix OS targets under both data and executable modes.
func TestValidatePathSecurityOS(t *testing.T) {
	t.Run("unix rules", func(t *testing.T) {
		tests := []struct {
			path            string
			allowSystemDirs bool
			wantErr         bool
		}{
			// Basic Unix restrictions (always restricted)
			{"/", false, true},
			{"/", true, true},
			{"/file.txt", false, true},
			{"/file.txt", true, true},

			// System directories (conditionally allowed)
			{"/etc", false, true},
			{"/etc", true, true}, // directly under root, needs at least 2 segments
			{"/etc/hosts", false, true},
			{"/etc/hosts", true, false},
			{"/usr/bin/go", false, true},
			{"/usr/bin/go", true, false},
			{"/bin/sh", false, true},
			{"/bin/sh", true, false},

			// Normal workspace path (always allowed)
			{"/data/workspace/file.txt", false, false},
			{"/data/workspace/file.txt", true, false},
		}
		for _, tt := range tests {
			err := validatePathSecurityOS(tt.path, "linux", tt.allowSystemDirs)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePathSecurityOS(%q, \"linux\", %v) error = %v, wantErr %v", tt.path, tt.allowSystemDirs, err, tt.wantErr)
			}
		}
	})

	t.Run("windows rules", func(t *testing.T) {
		tests := []struct {
			path            string
			allowSystemDirs bool
			wantErr         bool
		}{
			{`C:\`, false, true},
			{`C:\`, true, true},
			{`C:\file.txt`, false, true},
			{`C:\file.txt`, true, true},
			{`\\server\share`, false, true},
			{`\\server\share`, true, true},
			{`\\server\share\`, false, true},
			{`\\server\share\`, true, true},
			{`\\server\C$`, false, true},
			{`\\server\C$`, true, true},
			{`\\server\C$\file.txt`, false, true},
			{`\\server\C$\file.txt`, true, true},
			{`\\server\share\dir\file.txt`, false, false},
			{`\\server\share\dir\file.txt`, true, false},
			{`C:\data\file.txt`, false, false},
			{`C:\data\file.txt`, true, false},
			{`invalid_format`, false, true},
			{`invalid_format`, true, true},
		}
		for _, tt := range tests {
			err := validatePathSecurityOS(tt.path, "windows", tt.allowSystemDirs)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePathSecurityOS(%q, \"windows\", %v) error = %v, wantErr %v", tt.path, tt.allowSystemDirs, err, tt.wantErr)
			}
		}
	})
}

// TestSanitizePath_ValidPaths tests that valid paths are accepted.
func TestSanitizePath_ValidPaths(t *testing.T) {
	var validPaths []string

	if runtime.GOOS == "windows" {
		validPaths = []string{
			"C:\\data\\workspace\\file.txt",
			"D:\\mft\\batch001\\output.json",
			"C:\\Program Files\\App\\data.txt",
		}
	} else {
		validPaths = []string{
			"/data/workspace/file.txt",
			"/var/mft/batch001/output.json",
			"/opt/myapp/data/results.tsv",
			"/home/user/workspace/file.pdf",
		}
	}

	for _, path := range validPaths {
		t.Run(path, func(t *testing.T) {
			result, err := sanitizePath(path)
			if err != nil {
				t.Errorf("sanitizePath(%q) failed: %v", path, err)
				return
			}
			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

// TestValidateExecutable_NotExists tests non-existent executable.
func TestValidateExecutable_NotExists(t *testing.T) {
	err := validateExecutable("/nonexistent/path/to/binary")
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
}

// TestValidateExecutable_Directory tests that directories are rejected.
func TestValidateExecutable_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	err := validateExecutable(tmpDir)
	if err == nil {
		t.Error("expected error for directory, got nil")
	}
}

// TestValidateExecutable_NotExecutable tests non-executable files on Unix.
func TestValidateExecutable_NotExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping Unix-specific test on Windows")
	}

	tmpDir := t.TempDir()
	notExec := filepath.Join(tmpDir, "notexec")
	if err := os.WriteFile(notExec, []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	err := validateExecutable(notExec)
	if err == nil {
		t.Error("expected error for non-executable file, got nil")
	}
}

// TestWriteJSON_Success tests successful JSON writing.
func TestWriteJSON_Success(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	results := []Result{{Filename: "test.pdf", Value: "value"}}
	err := writeJSON(w, results)
	if err != nil {
		t.Errorf("writeJSON should not fail with valid buffer: %v", err)
	}
}

type errorWriter struct{}

func (errorWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("simulated write error")
}

// TestWriteJSON_Error tests JSON write error handling when the writer fails.
func TestWriteJSON_Error(t *testing.T) {
	ew := errorWriter{}
	w := bufio.NewWriterSize(ew, 1) // 1-byte buffer to trigger flush immediately

	results := []Result{{Filename: "test.pdf", Value: "value"}}
	err := writeJSON(w, results)
	if err == nil {
		t.Error("expected error for failing writer, got nil")
	}
}

// TestRunDetect_PathIsFile tests detection when path is a file, not directory.
func TestRunDetect_PathIsFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(tmpFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cfg := Config{
		Path:        tmpFile,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Format:      "json",
		Output:      filepath.Join(tmpDir, "out.json"),
	}

	code, err := runDetect(cfg)
	if code != ExitPathError {
		t.Errorf("expected exit code %d, got %d", ExitPathError, code)
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// createTestPDFWithContent creates a minimal PDF with text content for testing.
// Uses mutool to create a valid PDF from text input.
func createTestPDFWithContent(t *testing.T, path string, content string) {
	t.Helper()

	mutoolPath, _ := findMutool("")
	if mutoolPath == "" {
		// If mutool not available, create a simple text file
		// (tests will be skipped anyway due to mutool check)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		return
	}

	// Create a text file with content
	tmpDir := t.TempDir()
	textFile := filepath.Join(tmpDir, "input.txt")
	if err := os.WriteFile(textFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create text file: %v", err)
	}

	// For testing, we just need a file that mutool can process
	// Create a minimal PDF structure manually
	pdfContent := fmt.Sprintf(`%%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >> endobj
4 0 obj << /Length %d >> stream
BT /F1 12 Tf 100 700 Td (%s) Tj ET
endstream endobj
xref
0 5
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000214 00000 n
trailer << /Size 5 /Root 1 0 R >>
startxref
%d
%%%%EOF`, len(content)+50, content, 300+len(content))

	if err := os.WriteFile(path, []byte(pdfContent), 0o644); err != nil {
		t.Fatalf("failed to create PDF file: %v", err)
	}
}

// requireMutool locates the mutool binary or skips the test if not found.
func requireMutool(t *testing.T) string {
	t.Helper()
	mutoolPath, err := findMutool("")
	if err != nil {
		t.Skipf("mutool not available: %v", err)
	}
	return mutoolPath
}

// TestMain_Version tests the -version flag execution.
func TestMain_Version(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		// Reset the default flag set to avoid panics on redefinition
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		os.Args = []string{"go-pdf-extractor", "-version"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Version")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("process exited with error: %v, output: %s", err, string(output))
	}
	if !strings.Contains(string(output), "version") {
		t.Errorf("expected version output, got: %s", string(output))
	}
}

// TestIntegration_MultipleMatches verifies that a PDF containing multiple matches
// is processed correctly and returns a slice of values.
func TestIntegration_MultipleMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mutoolPath := requireMutool(t)

	tmpDir := t.TempDir()
	testPDF := filepath.Join(tmpDir, "multiple_matches.pdf")

	// Create a PDF with multiple matches on separate lines using text positioning operators
	content := "DSFN:value1) Tj 0 -15 Td (DSFN:value2"
	createTestPDFWithContent(t, testPDF, content)

	result := processFile(testPDF, mutoolPath, "DSFN:", 30*time.Second)
	if result.Error != "" {
		t.Fatalf("processFile failed: %s", result.Error)
	}

	values, ok := result.Value.([]string)
	if !ok {
		t.Fatalf("expected []string value for multiple matches, got %T (%v)", result.Value, result.Value)
	}

	if len(values) != 2 || values[0] != "value1" || values[1] != "value2" {
		t.Errorf("expected values [value1, value2], got %v", values)
	}
}

// TestKillProcessGroup_NilProcess verifies that killProcessGroup handles nil Process field without error.
func TestKillProcessGroup_NilProcess(t *testing.T) {
	cmd := &exec.Cmd{}
	err := killProcessGroup(cmd)
	if err != nil {
		t.Errorf("expected nil error for nil Process, got: %v", err)
	}
}

// TestKillProcessGroup_AlreadyFinished verifies that killProcessGroup returns nil error if the process already exited.
func TestKillProcessGroup_AlreadyFinished(t *testing.T) {
	var name string
	var args []string
	if runtime.GOOS == "windows" {
		name = "cmd"
		args = []string{"/c", "echo", "hello"}
	} else {
		name = "echo"
		args = []string{"hello"}
	}

	cmd := exec.Command(name, args...)
	setupProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("failed to wait command: %v", err)
	}

	// Now that it's finished, call killProcessGroup
	err := killProcessGroup(cmd)
	if err != nil {
		t.Errorf("expected nil error for already finished process, got: %v", err)
	}
}

// TestKillProcessGroup_Running verifies that killProcessGroup can kill a running process without returning an error.
func TestKillProcessGroup_Running(t *testing.T) {
	var name string
	var args []string
	if runtime.GOOS == "windows" {
		name = "ping"
		args = []string{"127.0.0.1", "-n", "10"}
	} else {
		name = "sleep"
		args = []string{"10"}
	}

	cmd := exec.Command(name, args...)
	setupProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	// Kill the running process
	err := killProcessGroup(cmd)
	if err != nil {
		t.Errorf("expected nil error for killing running process, got: %v", err)
	}

	// Wait should return an error because it was killed
	waitErr := cmd.Wait()
	if waitErr == nil {
		t.Error("expected process to be terminated with error, but exit code was 0")
	}
}
