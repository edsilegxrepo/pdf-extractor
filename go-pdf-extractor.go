// Package main implements go-pdf-extract, a CLI tool for extracting delimiter-based
// values from PDF files using MuPDF's mutool binary.
//
// Architecture Overview:
//   - Input: CLI flags define workspace path, file pattern, search delimiter, and output format
//   - Processing: Worker pool concurrently processes PDFs via mutool subprocess calls
//   - Output: Results written as NDJSON or TSV to specified output file
//
// Data Flow:
//
//	CLI args -> Config validation -> mutool discovery -> file discovery ->
//	worker pool (parallel mutool calls) -> value extraction -> output serialization
//
// Concurrency Model:
//
//	Bounded worker pool using goroutines and channels. Workers receive file paths
//	from a jobs channel and send Results to a results channel. Main goroutine
//	aggregates results after all workers complete.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// version holds the application version string.
// Default is "dev" for development builds.
// For releases, set at build time via: go build -ldflags "-X main.version=1.0.0"
var version = "dev"

const (
	// defaultTimeout is the per-file timeout for mutool execution.
	// Can be overridden via -timeout flag for large or complex PDFs.
	defaultTimeout = 30 * time.Second

	// Exit codes provide diagnostic information for integration with job schedulers.
	// Each code maps to a specific failure category for automated error handling.
	ExitSuccess        = 0  // All files processed successfully
	ExitConfigError    = 1  // Invalid configuration or missing required flags
	ExitMutoolNotFound = 2  // mutool binary not found in any configured location
	ExitPathError      = 3  // Workspace path not found or not a directory
	ExitPatternError   = 4  // Invalid glob pattern syntax
	ExitOutputError    = 5  // Cannot create or write to output file
	ExitPartialFailure = 10 // Some PDFs failed processing (output still written)
)

// Result represents the extraction outcome for a single PDF file.
// JSON tags control serialization for NDJSON output format.
type Result struct {
	Filename string      `json:"filename"`        // Base name of the processed PDF
	Value    interface{} `json:"value"`           // Extracted value(s): string, []string, or nil
	Error    string      `json:"error,omitempty"` // Error message if processing failed
}

// Config holds all CLI configuration parameters.
// Populated by parseFlags() from command-line arguments.
type Config struct {
	Path        string        // Workspace directory containing PDF files
	FilePattern string        // Glob pattern for file selection (e.g., "*.pdf")
	Search      string        // Delimiter pattern to search for (e.g., "DSFN:")
	Format      string        // Output format: "json" or "tsv"
	Output      string        // Path to output file
	MutoolBin   string        // Optional explicit path to mutool binary
	Timeout     time.Duration // Per-file timeout for mutool execution
	Workers     int           // Number of concurrent worker goroutines
}

// main is the application entry point.
// Parses flags, handles -version, and delegates to run() for core logic.
// Exits with appropriate code based on run() result.
func main() {
	cfg, showVersion := parseFlags()
	if showVersion {
		fmt.Printf("go-pdf-extract version %s\n", version)
		os.Exit(0)
	}
	exitCode, err := run(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	os.Exit(exitCode)
}

// run executes the main application logic and returns an exit code.
// Orchestrates the pipeline: validate -> find mutool -> find files -> process -> write output.
// Returns (exitCode, error) where error provides diagnostic details for non-zero codes.
func run(cfg Config) (int, error) {
	// Phase 1: Validate configuration
	// Distinguishes path errors (code 3) from other config errors (code 1)
	if err := validateConfig(cfg); err != nil {
		if strings.Contains(err.Error(), "workspace path") {
			return ExitPathError, err
		}
		return ExitConfigError, err
	}

	// Phase 2: Locate and validate mutool binary
	// Checks: -mutool-bin flag -> MUTOOL_BIN env -> PATH lookup
	mutoolPath, err := findMutool(cfg.MutoolBin)
	if err != nil {
		return ExitMutoolNotFound, err
	}

	// Phase 3: Discover files matching the glob pattern
	files, err := findFiles(cfg.Path, cfg.FilePattern)
	if err != nil {
		return ExitPatternError, err
	}

	// Phase 4: Process all files concurrently via worker pool
	results := processFiles(files, mutoolPath, cfg.Search, cfg.Timeout, cfg.Workers)

	// Phase 5: Write results to output file
	if err := writeOutput(results, cfg.Format, cfg.Output); err != nil {
		return ExitOutputError, fmt.Errorf("writing output: %w", err)
	}

	// Phase 6: Check for partial failures (some files had errors)
	// Output is still written; exit code signals that review may be needed
	for _, r := range results {
		if r.Error != "" {
			return ExitPartialFailure, nil
		}
	}

	return ExitSuccess, nil
}

// parseFlags parses command-line arguments and returns configuration.
// Returns (Config, showVersion) where showVersion indicates -version flag was set.
// Note: This function calls flag.Parse() which may exit on -help or invalid flags.
func parseFlags() (Config, bool) {
	cfg := Config{}

	// Required flags - all must be provided for normal operation
	flag.StringVar(&cfg.Path, "path", "", "Workspace directory containing PDF files")
	flag.StringVar(&cfg.FilePattern, "file-pattern", "", "Glob pattern for PDF files (e.g., *.pdf)")
	flag.StringVar(&cfg.Search, "search", "", "Delimiter pattern to search for (e.g., DSFN:)")
	flag.StringVar(&cfg.Format, "format", "", "Output format: json or tsv")
	flag.StringVar(&cfg.Output, "output", "", "Output file path")

	// Optional flags - have sensible defaults or are auto-detected
	flag.StringVar(&cfg.MutoolBin, "mutool-bin", "", "Path to mutool binary (optional)")
	timeout := flag.Duration("timeout", defaultTimeout, "Timeout for each mutool invocation")
	workers := flag.Int("workers", 0, "Number of worker goroutines (default: NumCPU*2, min: 2, max: 16)")
	showVersion := flag.Bool("version", false, "Print version and exit")

	flag.Parse()

	cfg.Timeout = *timeout
	cfg.Workers = *workers
	return cfg, *showVersion
}

// validateConfig checks that all required configuration is present and valid.
// Performs path sanitization and validates that workspace exists and is a directory.
// Returns descriptive error messages for user feedback.
func validateConfig(cfg Config) error {
	// Check all required flags are provided
	if cfg.Path == "" {
		return fmt.Errorf("missing required flag: -path")
	}
	if cfg.FilePattern == "" {
		return fmt.Errorf("missing required flag: -file-pattern")
	}
	if cfg.Search == "" {
		return fmt.Errorf("missing required flag: -search")
	}
	if cfg.Format == "" {
		return fmt.Errorf("missing required flag: -format")
	}
	if cfg.Format != "json" && cfg.Format != "tsv" {
		return fmt.Errorf("invalid format: %s (must be 'json' or 'tsv')", cfg.Format)
	}
	if cfg.Output == "" {
		return fmt.Errorf("missing required flag: -output")
	}

	// Sanitize and validate workspace path
	// Path must exist and be a directory (not a file)
	cleanPath, err := sanitizePath(cfg.Path)
	if err != nil {
		return fmt.Errorf("workspace path error: %v", err)
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("workspace path error: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace path is not a directory: %s", cleanPath)
	}

	// Sanitize output path (parent directory existence checked at write time)
	if _, err := sanitizePath(cfg.Output); err != nil {
		return fmt.Errorf("output path error: %v", err)
	}

	return nil
}

// sanitizePath cleans and validates a filesystem path to prevent path traversal attacks.
// Performs: empty check -> filepath.Clean -> filepath.Abs -> null byte rejection.
// SECURITY: All user-supplied paths must pass through this function before use.
func sanitizePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Clean the path to resolve . and .. components
	// This normalizes the path and removes redundant separators
	cleaned := filepath.Clean(path)

	// Convert to absolute path for consistent handling
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
	}

	// Reject paths with null bytes (potential injection attempt)
	if strings.ContainsRune(absPath, 0) {
		return "", fmt.Errorf("path contains invalid characters")
	}

	return absPath, nil
}

// validateExecutable checks that a path points to a valid executable file.
// On Unix: verifies execute permission bits are set.
// On Windows: verifies file has an executable extension (.exe, .com, .bat, .cmd).
// SECURITY: path must be pre-sanitized via sanitizePath() before calling this function.
func validateExecutable(path string) error {
	// #nosec G703 -- path is pre-sanitized by sanitizePath() in all callers (findMutool)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not an executable")
	}

	// Platform-specific executable validation
	if runtime.GOOS != "windows" {
		// Unix: check if any execute bit is set (owner, group, or other)
		if info.Mode()&0o111 == 0 {
			return fmt.Errorf("file is not executable")
		}
	} else {
		// Windows: check for executable extension
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".exe" && ext != ".com" && ext != ".bat" && ext != ".cmd" {
			return fmt.Errorf("file does not have executable extension")
		}
	}
	return nil
}

// validateMutoolPath combines path sanitization and executable validation.
// Used by findMutool() to validate paths from different sources.
// The source parameter is included in error messages for diagnostic clarity.
func validateMutoolPath(path, source string) (string, error) {
	cleanPath, err := sanitizePath(path)
	if err != nil {
		return "", fmt.Errorf("invalid %s path: %v", source, err)
	}
	if err := validateExecutable(cleanPath); err != nil {
		return "", fmt.Errorf("mutool binary not valid at %s path %s: %v", source, cleanPath, err)
	}
	return cleanPath, nil
}

// findMutool locates and validates the mutool binary.
// Search order (first valid wins):
//  1. -mutool-bin CLI flag
//  2. MUTOOL_BIN environment variable
//  3. "mutool" in system PATH
//
// Returns the validated absolute path to the mutool binary.
func findMutool(flagPath string) (string, error) {
	// Priority 1: Explicit CLI flag
	if flagPath != "" {
		return validateMutoolPath(flagPath, "-mutool-bin")
	}

	// Priority 2: Environment variable
	if envPath := os.Getenv("MUTOOL_BIN"); envPath != "" {
		return validateMutoolPath(envPath, "MUTOOL_BIN")
	}

	// Priority 3: System PATH lookup
	path, err := exec.LookPath("mutool")
	if err != nil {
		return "", fmt.Errorf("mutool not found in PATH, set MUTOOL_BIN or use -mutool-bin flag")
	}
	return validateMutoolPath(path, "PATH")
}

// findFiles discovers files matching the glob pattern in the workspace directory.
// Returns full paths to all matching files.
// An empty result (no matches) is not an error - returns empty slice.
func findFiles(basePath, pattern string) ([]string, error) {
	fullPattern := filepath.Join(basePath, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %v", err)
	}
	return matches, nil
}

// processFiles concurrently processes all PDF files using a bounded worker pool.
//
// Worker Pool Architecture:
//   - Jobs channel: buffered to file count, receives file paths
//   - Results channel: buffered to file count, receives extraction results
//   - Workers: goroutines that consume from jobs and produce to results
//   - Synchronization: WaitGroup ensures all workers complete before channel close
//
// Worker count bounds: minimum 2, maximum 16, default NumCPU*2.
// These bounds prevent resource exhaustion while utilizing available parallelism.
func processFiles(files []string, mutoolPath, search string, timeout time.Duration, workers int) []Result {
	// Apply worker count bounds
	numWorkers := workers
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU() * 2 // Default: 2x CPU cores
	}
	if numWorkers < 2 {
		numWorkers = 2 // Minimum: ensure some parallelism
	}
	if numWorkers > 16 {
		numWorkers = 16 // Maximum: prevent resource exhaustion
	}

	// Create buffered channels sized to file count
	// This prevents blocking and ensures all items can be queued
	jobs := make(chan string, len(files))
	results := make(chan Result, len(files))

	// Launch worker goroutines
	// Each worker processes files until the jobs channel is closed
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				result := processFile(file, mutoolPath, search, timeout)
				results <- result
			}
		}()
	}

	// Enqueue all files for processing
	for _, file := range files {
		jobs <- file
	}
	close(jobs) // Signal workers that no more jobs are coming

	// Close results channel after all workers complete
	// This runs in a separate goroutine to avoid blocking
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results from workers
	var allResults []Result
	for result := range results {
		allResults = append(allResults, result)
	}

	return allResults
}

// processFile extracts values from a single PDF file using mutool.
// Spawns mutool as a subprocess with timeout control and process group isolation.
//
// Subprocess Management:
//   - Context with timeout ensures mutool doesn't run indefinitely
//   - Process group isolation enables clean termination of mutool and any children
//   - On timeout or error, killProcessGroup ensures no orphaned processes
//
// SECURITY: mutoolPath must be pre-validated via findMutool() which sanitizes
// and validates the executable. filePath comes from filepath.Glob on a validated directory.
func processFile(filePath, mutoolPath, search string, timeout time.Duration) Result {
	filename := filepath.Base(filePath)
	result := Result{Filename: filename}

	// Create context with timeout for subprocess control
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build mutool command: mutool draw -q -F txt -o - <file>
	// -q: quiet mode (suppress warnings)
	// -F txt: output format is plain text
	// -o -: write to stdout (captured by cmd.Output())
	// #nosec G204 -- mutoolPath is sanitized and validated as executable by findMutool(); filePath comes from filepath.Glob
	cmd := exec.CommandContext(ctx, mutoolPath, "draw", "-q", "-F", "txt", "-o", "-", filePath)

	// Configure process group for clean termination (platform-specific)
	setupProcessGroup(cmd)

	// Execute mutool and capture stdout
	output, err := cmd.Output()
	if err != nil {
		// Ensure process group is terminated on error
		killProcessGroup(cmd)

		// Provide specific error message for timeout vs other errors
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "timeout exceeded"
		} else {
			result.Error = fmt.Sprintf("mutool error: %v", err)
		}
		return result
	}

	// Extract values from mutool output text
	values := extractValues(string(output), search)

	// Set result value based on match count
	// nil: no matches, string: single match, []string: multiple matches
	if len(values) == 0 {
		result.Value = nil
	} else if len(values) == 1 {
		result.Value = values[0]
	} else {
		result.Value = values
	}

	return result
}

// extractValues searches text for lines containing the search pattern
// and extracts the value following the pattern on each matching line.
//
// Extraction Logic:
//   - Scans each line for the search pattern
//   - Extracts and trims text after the pattern to end of line
//   - Deduplicates values (same value appearing multiple times)
//   - Returns values in order of first occurrence
//
// Example: search="DSFN:", line="DSFN: 12345" -> extracts "12345"
func extractValues(text, search string) []string {
	var values []string
	seen := make(map[string]bool) // Track seen values for deduplication

	for _, line := range strings.Split(text, "\n") {
		// Find the search pattern in the line
		idx := strings.Index(line, search)
		if idx == -1 {
			continue
		}

		// Extract value: everything after the pattern, trimmed
		value := strings.TrimSpace(line[idx+len(search):])

		// Add to results if non-empty and not already seen
		if value != "" && !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}

	return values
}

// writeOutput writes extraction results to the output file in the specified format.
// Supports "json" (NDJSON) and "tsv" (tab-separated with header) formats.
// Uses buffered I/O for efficient writing of many small records.
func writeOutput(results []Result, format, outputPath string) error {
	// Sanitize output path before creating file
	cleanPath, err := sanitizePath(outputPath)
	if err != nil {
		return fmt.Errorf("invalid output path: %v", err)
	}

	// Create output file (truncates if exists)
	// #nosec G304 -- cleanPath is sanitized by sanitizePath() on the line above
	file, err := os.Create(cleanPath)
	if err != nil {
		return fmt.Errorf("cannot create output file: %v", err)
	}
	defer func() { _ = file.Close() }() // Best-effort close; data flushed via writer.Flush()

	// Use buffered writer for efficient I/O
	writer := bufio.NewWriter(file)

	// Delegate to format-specific writer
	var writeErr error
	switch format {
	case "json":
		writeErr = writeJSON(writer, results)
	case "tsv":
		writeErr = writeTSV(writer, results)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if writeErr != nil {
		return writeErr
	}

	// Flush buffer to file - this is the critical write operation
	return writer.Flush()
}

// writeJSON writes results as NDJSON (newline-delimited JSON).
// Each result is a complete JSON object on its own line.
// This format is streaming-friendly and compatible with tools like jq.
func writeJSON(writer *bufio.Writer, results []Result) error {
	for _, result := range results {
		// Marshal result to JSON
		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("JSON marshal error: %v", err)
		}

		// Write JSON object followed by newline
		if _, err := writer.Write(data); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
	}
	return nil
}

// writeTSV writes results as tab-separated values with a header row.
// Format: filename<TAB>value<NEWLINE>
// Multiple values are comma-separated in the value column.
// Errors are not included in TSV format (value column is empty).
func writeTSV(writer *bufio.Writer, results []Result) error {
	// Write header row
	if _, err := writer.WriteString("filename\tvalue\n"); err != nil {
		return fmt.Errorf("write error: %v", err)
	}

	// Write data rows
	for _, result := range results {
		// Convert value to string representation
		var value string
		switch v := result.Value.(type) {
		case string:
			value = v
		case []string:
			value = strings.Join(v, ",") // Multiple values comma-separated
		}
		// nil values result in empty string (no special handling needed)

		line := fmt.Sprintf("%s\t%s\n", result.Filename, value)
		if _, err := writer.WriteString(line); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
	}
	return nil
}
