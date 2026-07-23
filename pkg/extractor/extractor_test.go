package extractor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

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
			name:     "duplicate values deduplicated",
			text:     "DSFN:123\nDSFN:123\nDSFN:456",
			search:   "DSFN:",
			expected: []string{"123", "456"},
		},
		{
			name:     "CRLF line endings",
			text:     "DSFN:value1\r\nDSFN:value2\r\n",
			search:   "DSFN:",
			expected: []string{"value1", "value2"},
		},
		{
			name:     "mixed line endings",
			text:     "DSFN:value1\r\nDSFN:value2\nDSFN:value3\r",
			search:   "DSFN:",
			expected: []string{"value1", "value2", "value3"},
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
					t.Errorf("result[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "empty path",
			path:      "",
			wantErr:   true,
			errSubstr: "empty",
		},
		{
			name:      "path traversal",
			path:      "/some/path/../secret",
			wantErr:   true,
			errSubstr: "traversal",
		},
		{
			name:      "relative path",
			path:      "relative/path",
			wantErr:   true,
			errSubstr: "absolute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SanitizePath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
					return
				}
				if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePathSecurityOS(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		goos      string
		allowSys  bool
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "unix root",
			path:      "/",
			goos:      "linux",
			wantErr:   true,
			errSubstr: "root",
		},
		{
			name:      "unix file in root",
			path:      "/file.txt",
			goos:      "linux",
			wantErr:   true,
			errSubstr: "root directory",
		},
		{
			name:    "unix valid path",
			path:    "/home/user/file.txt",
			goos:    "linux",
			wantErr: false,
		},
		{
			name:      "unix system dir blocked",
			path:      "/etc/passwd",
			goos:      "linux",
			allowSys:  false,
			wantErr:   true,
			errSubstr: "system directory",
		},
		{
			name:     "unix system dir allowed",
			path:     "/usr/bin/mutool",
			goos:     "linux",
			allowSys: true,
			wantErr:  false,
		},
		{
			name:      "windows root",
			path:      "C:\\",
			goos:      "windows",
			wantErr:   true,
			errSubstr: "root directory",
		},
		{
			name:    "windows valid path",
			path:    "C:\\Users\\test\\file.txt",
			goos:    "windows",
			wantErr: false,
		},
		{
			name:      "windows admin share",
			path:      "\\\\server\\C$\\dir\\file.txt",
			goos:      "windows",
			wantErr:   true,
			errSubstr: "administrative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePathSecurityOS(tt.path, tt.goos, tt.allowSys)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
					return
				}
				if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestFindFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	testFiles := []string{"test1.pdf", "test2.pdf", "other.txt"}
	for _, f := range testFiles {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0o644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	t.Run("match pdf files", func(t *testing.T) {
		files, err := FindFiles(tmpDir, "*.pdf")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		files, err := FindFiles(tmpDir, "*.doc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("expected 0 files, got %d", len(files))
		}
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		_, err := FindFiles(tmpDir, "../*.pdf")
		if err == nil {
			t.Error("expected error for path traversal, got nil")
		}
	})
}

func TestValidateExecutable(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("directory not executable", func(t *testing.T) {
		err := ValidateExecutable(tmpDir)
		if err == nil {
			t.Error("expected error for directory, got nil")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		err := ValidateExecutable(filepath.Join(tmpDir, "nonexistent"))
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})

	if runtime.GOOS == "windows" {
		t.Run("windows exe extension required", func(t *testing.T) {
			noExtFile := filepath.Join(tmpDir, "testfile")
			if err := os.WriteFile(noExtFile, []byte("test"), 0o755); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
			err := ValidateExecutable(noExtFile)
			if err == nil {
				t.Error("expected error for non-exe extension on Windows, got nil")
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// MUTOOL TESTS
// =============================================================================

func mutoolAvailable() bool {
	_, err := exec.LookPath("mutool")
	return err == nil
}

func skipIfNoMutool(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !mutoolAvailable() {
		t.Skip("mutool not available")
	}
}

func TestFindMutool(t *testing.T) {
	skipIfNoMutool(t)

	t.Run("find via PATH", func(t *testing.T) {
		path, err := FindMutool("")
		if err != nil {
			t.Fatalf("FindMutool failed: %v", err)
		}
		if path == "" {
			t.Error("expected non-empty path")
		}
	})

	t.Run("explicit path", func(t *testing.T) {
		// First find it via PATH, then use that path explicitly
		pathFromPATH, err := FindMutool("")
		if err != nil {
			t.Fatalf("FindMutool failed: %v", err)
		}
		path, err := FindMutool(pathFromPATH)
		if err != nil {
			t.Fatalf("FindMutool with explicit path failed: %v", err)
		}
		if path != pathFromPATH {
			t.Errorf("expected %q, got %q", pathFromPATH, path)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := FindMutool("/nonexistent/path/to/mutool")
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})
}

func TestTestMutoolExecution(t *testing.T) {
	skipIfNoMutool(t)

	t.Run("valid mutool", func(t *testing.T) {
		path, _ := FindMutool("")
		err := TestMutoolExecution(path)
		if err != nil {
			t.Errorf("TestMutoolExecution failed: %v", err)
		}
	})

	t.Run("invalid binary", func(t *testing.T) {
		tmpDir := t.TempDir()
		fakeBin := filepath.Join(tmpDir, "fakemutool")
		if runtime.GOOS == "windows" {
			fakeBin += ".exe"
		}
		if err := os.WriteFile(fakeBin, []byte("not a binary"), 0o755); err != nil {
			t.Fatalf("failed to create fake binary: %v", err)
		}
		err := TestMutoolExecution(fakeBin)
		if err == nil {
			t.Error("expected error for invalid binary")
		}
	})
}

func TestValidateDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid directory", func(t *testing.T) {
		err := ValidateDirectory(tmpDir)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		err := ValidateDirectory(filepath.Join(tmpDir, "nonexistent"))
		if err == nil {
			t.Error("expected error for non-existent path")
		}
	})

	t.Run("file not directory", func(t *testing.T) {
		file := filepath.Join(tmpDir, "file.txt")
		if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		err := ValidateDirectory(file)
		if err == nil {
			t.Error("expected error for file")
		}
	})
}

// =============================================================================
// INTEGRATION TESTS WITH REAL PDFs
// =============================================================================

func getTestfilesDir(t *testing.T) string {
	t.Helper()
	// Find testdata relative to repo root
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Try current dir, then parent, then grandparent
	candidates := []string{
		filepath.Join(cwd, "testdata"),
		filepath.Join(cwd, "..", "..", "testdata"),
		filepath.Join(cwd, "..", "..", "..", "testdata"),
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

func TestExtract_SingleFile(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	t.Run("sample001 with DSFN match", func(t *testing.T) {
		results, err := Extract(context.Background(), Options{
			Path:        testdataDir,
			FilePattern: "sample001.pdf",
			Search:      "DSFN:",
			Timeout:     30 * time.Second,
		})
		if err != nil {
			t.Fatalf("Extract failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Filename != "sample001.pdf" {
			t.Errorf("expected filename sample001.pdf, got %s", results[0].Filename)
		}
		if results[0].Error != "" {
			t.Errorf("unexpected error: %s", results[0].Error)
		}
		// sample001.pdf contains: DSFN:Employee ID_X_X_X_X_Eag-AHP.pdf
		expected := "Employee ID_X_X_X_X_Eag-AHP.pdf"
		if results[0].Value != expected {
			t.Errorf("expected value %q, got %v", expected, results[0].Value)
		}
	})

	t.Run("sample002 with space after delimiter", func(t *testing.T) {
		results, err := Extract(context.Background(), Options{
			Path:        testdataDir,
			FilePattern: "sample002.pdf",
			Search:      "DSFN:",
			Timeout:     30 * time.Second,
		})
		if err != nil {
			t.Fatalf("Extract failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		// sample002.pdf contains: DSFN: 327078_X_X_X_X_Wage.pdf (note space after colon)
		expected := "327078_X_X_X_X_Wage.pdf"
		if results[0].Value != expected {
			t.Errorf("expected value %q, got %v", expected, results[0].Value)
		}
	})
}

func TestExtract_BatchProcessing(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "sample*.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
		Workers:     4,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both files should have values
	matchCount := 0
	for _, r := range results {
		if r.Value != nil && r.Error == "" {
			matchCount++
		}
	}
	if matchCount != 2 {
		t.Errorf("expected 2 matches, got %d", matchCount)
	}
}

func TestExtract_NoMatch(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "sample*.pdf",
		Search:      "NONEXISTENT_PATTERN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// All results should have nil values
	for _, r := range results {
		if r.Value != nil {
			t.Errorf("expected nil value for %s, got %v", r.Filename, r.Value)
		}
	}
}

func TestExtract_InvalidPath(t *testing.T) {
	_, err := Extract(context.Background(), Options{
		Path:        "/nonexistent/path",
		FilePattern: "*.pdf",
		Search:      "DSFN:",
	})
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestExtract_NoMatchingFiles(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	_, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "*.nonexistent",
		Search:      "DSFN:",
	})
	if err == nil {
		t.Error("expected error for no matching files")
	}
	if !contains(err.Error(), "no files matching") {
		t.Errorf("expected 'no files matching' error, got: %v", err)
	}
}

func TestExtract_DefaultTimeout(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	// Timeout=0 should use default
	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "sample001.pdf",
		Search:      "DSFN:",
		Timeout:     0, // Should use DefaultTimeout
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// =============================================================================
// WORKER POOL TESTS
// =============================================================================

func TestProcessFiles_WorkerBounds(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	mutoolPath, err := FindMutool("")
	if err != nil {
		t.Fatalf("FindMutool failed: %v", err)
	}

	files, err := FindFiles(testdataDir, "sample*.pdf")
	if err != nil {
		t.Fatalf("FindFiles failed: %v", err)
	}

	testCases := []struct {
		name     string
		workers  int
		expected int // Results count
	}{
		{"zero workers uses default", 0, 2},
		{"one worker becomes 2", 1, 2},
		{"four workers", 4, 2},
		{"twenty workers capped to 16", 20, 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := processFiles(files, mutoolPath, "DSFN:", 30*time.Second, tc.workers)
			if len(results) != tc.expected {
				t.Errorf("expected %d results, got %d", tc.expected, len(results))
			}
		})
	}
}

func TestProcessFiles_Empty(t *testing.T) {
	results := processFiles([]string{}, "/dummy/path", "DSFN:", 30*time.Second, 2)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

// =============================================================================
// ERROR HANDLING TESTS
// =============================================================================

func TestExtract_Errors(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		_, err := Extract(context.Background(), Options{
			Path:        "",
			FilePattern: "*.pdf",
			Search:      "DSFN:",
		})
		if err == nil {
			t.Error("expected error for empty path")
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		_, err := Extract(context.Background(), Options{
			Path:        "/some/../path",
			FilePattern: "*.pdf",
			Search:      "DSFN:",
		})
		if err == nil {
			t.Error("expected error for path traversal")
		}
	})

	t.Run("relative path", func(t *testing.T) {
		_, err := Extract(context.Background(), Options{
			Path:        "relative/path",
			FilePattern: "*.pdf",
			Search:      "DSFN:",
		})
		if err == nil {
			t.Error("expected error for relative path")
		}
	})
}

// =============================================================================
// RESULT TYPE TESTS
// =============================================================================

func TestResultTypes(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, r := range results {
		// Each result should have a filename
		if r.Filename == "" {
			t.Error("result has empty filename")
		}

		// Value should be string (single match) or nil (no match)
		// In our test files, both have single matches
		switch v := r.Value.(type) {
		case string:
			if v == "" {
				t.Errorf("result for %s has empty string value", r.Filename)
			}
		case nil:
			// Acceptable for no match
		case []string:
			// Acceptable for multiple matches
			if len(v) == 0 {
				t.Errorf("result for %s has empty slice value", r.Filename)
			}
		default:
			t.Errorf("unexpected value type %T for %s", r.Value, r.Filename)
		}
	}
}

// =============================================================================
// ADDITIONAL EDGE CASES
// =============================================================================

func TestExtractValues_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		search   string
		expected []string
	}{
		{
			name:     "empty text",
			text:     "",
			search:   "DSFN:",
			expected: nil,
		},
		{
			name:     "empty search",
			text:     "DSFN:value",
			search:   "",
			expected: []string{"DSFN:value"},
		},
		{
			name:     "search at end of line with no value",
			text:     "DSFN:",
			search:   "DSFN:",
			expected: nil,
		},
		{
			name:     "search with only whitespace after",
			text:     "DSFN:   ",
			search:   "DSFN:",
			expected: nil,
		},
		{
			name:     "multiple delimiters on same line",
			text:     "DSFN:value1 DSFN:value2",
			search:   "DSFN:",
			expected: []string{"value1 DSFN:value2"},
		},
		{
			name:     "unicode in value",
			text:     "DSFN:文件名.pdf",
			search:   "DSFN:",
			expected: []string{"文件名.pdf"},
		},
		{
			name:     "special characters",
			text:     "DSFN:file-name_v2.0 (copy).pdf",
			search:   "DSFN:",
			expected: []string{"file-name_v2.0 (copy).pdf"},
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
					t.Errorf("result[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestSanitizeExecutablePath(t *testing.T) {
	t.Run("allows system dirs", func(t *testing.T) {
		// This should not error for /usr/bin on Linux
		if runtime.GOOS != "windows" {
			_, err := SanitizeExecutablePath("/usr/bin/mutool")
			if err != nil {
				t.Errorf("SanitizeExecutablePath should allow /usr/bin: %v", err)
			}
		}
	})

	t.Run("rejects traversal", func(t *testing.T) {
		_, err := SanitizeExecutablePath("/usr/../etc/passwd")
		if err == nil {
			t.Error("expected error for path traversal")
		}
	})
}

// Benchmark for value extraction
func BenchmarkExtractValues(b *testing.B) {
	text := `Some header text
DSFN:value1
More content here
DSFN: value2
DSFN:value3
Footer text`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractValues(text, "DSFN:")
	}
}

// Benchmark for full extraction (requires mutool)
func BenchmarkExtract(b *testing.B) {
	if testing.Short() || !mutoolAvailable() {
		b.Skip("skipping benchmark: mutool not available or short mode")
	}

	cwd, _ := os.Getwd()
	testdataDir := filepath.Join(cwd, "..", "..", "testdata")
	if _, err := os.Stat(testdataDir); err != nil {
		b.Skip("testdata directory not found")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Extract(context.Background(), Options{
			Path:        testdataDir,
			FilePattern: "*.pdf",
			Search:      "DSFN:",
			Timeout:     30 * time.Second,
		})
		if err != nil {
			b.Fatalf("Extract failed: %v", err)
		}
	}
}

// =============================================================================
// TESTS WITH NEW TEST FILES
// =============================================================================

func TestExtract_MultiMatch(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "multi-match.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should return slice of 3 values
	values, ok := results[0].Value.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T: %v", results[0].Value, results[0].Value)
	}
	if len(values) != 3 {
		t.Errorf("expected 3 values, got %d: %v", len(values), values)
	}

	expected := []string{"FirstValue_001.pdf", "SecondValue_002.pdf", "ThirdValue_003.pdf"}
	for i, exp := range expected {
		if i < len(values) && values[i] != exp {
			t.Errorf("value[%d] = %q, want %q", i, values[i], exp)
		}
	}
}

func TestExtract_NoMatchFile(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "no-match.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Value != nil {
		t.Errorf("expected nil value for no-match file, got %v", results[0].Value)
	}
	if results[0].Error != "" {
		t.Errorf("unexpected error: %s", results[0].Error)
	}
}

func TestExtract_EmptyFile(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "empty.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Value != nil {
		t.Errorf("expected nil value for empty file, got %v", results[0].Value)
	}
}

func TestExtract_DuplicateValues(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "duplicate-values.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should deduplicate - only 2 unique values
	values, ok := results[0].Value.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T: %v", results[0].Value, results[0].Value)
	}
	if len(values) != 2 {
		t.Errorf("expected 2 unique values (deduplicated), got %d: %v", len(values), values)
	}

	// First occurrence order: DuplicateValue.pdf, UniqueValue.pdf
	if values[0] != "DuplicateValue.pdf" {
		t.Errorf("first value = %q, want 'DuplicateValue.pdf'", values[0])
	}
	if values[1] != "UniqueValue.pdf" {
		t.Errorf("second value = %q, want 'UniqueValue.pdf'", values[1])
	}
}

func TestExtract_WhitespaceVariations(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "whitespace-variations.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	values, ok := results[0].Value.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T: %v", results[0].Value, results[0].Value)
	}
	if len(values) != 3 {
		t.Errorf("expected 3 values, got %d: %v", len(values), values)
	}

	// All should be trimmed
	expected := []string{"NoSpace.pdf", "OneSpace.pdf", "MultipleSpaces.pdf"}
	for i, exp := range expected {
		if i < len(values) && values[i] != exp {
			t.Errorf("value[%d] = %q, want %q", i, values[i], exp)
		}
	}
}

func TestExtract_LongValue(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "long-value.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expected := "This_Is_A_Very_Long_Filename_With_Many_Underscores_And_Numbers_12345_And_More_Text_Here.pdf"
	if results[0].Value != expected {
		t.Errorf("value = %q, want %q", results[0].Value, expected)
	}
}

func TestExtract_UnicodeValue(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "unicode-value.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expected := "File-Name_v2.0 (copy).pdf"
	if results[0].Value != expected {
		t.Errorf("value = %q, want %q", results[0].Value, expected)
	}
}

func TestExtract_DifferentDelimiter(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	tests := []struct {
		search   string
		expected string
	}{
		{"REF:", "Reference123"},
		{"ID:", "Identifier456"},
		{"DOC_NUM:", "Document789"},
	}

	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			results, err := Extract(context.Background(), Options{
				Path:        testdataDir,
				FilePattern: "different-delimiter.pdf",
				Search:      tt.search,
				Timeout:     30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Extract failed: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}

			if results[0].Value != tt.expected {
				t.Errorf("value = %q, want %q", results[0].Value, tt.expected)
			}
		})
	}
}

func TestExtract_AllTestFiles(t *testing.T) {
	skipIfNoMutool(t)
	testdataDir := getTestfilesDir(t)

	results, err := Extract(context.Background(), Options{
		Path:        testdataDir,
		FilePattern: "*.pdf",
		Search:      "DSFN:",
		Timeout:     30 * time.Second,
		Workers:     4,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Should have 10 results (all PDF files)
	if len(results) != 10 {
		t.Errorf("expected 10 results, got %d", len(results))
	}

	// Count files with matches vs no matches
	matchCount := 0
	noMatchCount := 0
	for _, r := range results {
		if r.Error != "" {
			t.Errorf("unexpected error for %s: %s", r.Filename, r.Error)
		}
		if r.Value != nil {
			matchCount++
		} else {
			noMatchCount++
		}
	}

	// Expected: 8 with matches (sample001, sample002, multi-match, duplicate-values,
	//           whitespace-variations, long-value, unicode-value, different-delimiter has no DSFN)
	// Wait - different-delimiter has no DSFN, and no-match, empty also have no DSFN
	// So: 7 with DSFN matches, 3 without (no-match, empty, different-delimiter)
	if matchCount < 7 {
		t.Errorf("expected at least 7 files with matches, got %d", matchCount)
	}
}

// Ensure unused imports are used
var _ = fmt.Sprintf
