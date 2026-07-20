// Package main implements go-pdf-extractor, a CLI tool for extracting delimiter-based
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
	ExitSuccess     = 0 // All files processed successfully
	ExitConfigError = 1 // Invalid configuration or missing required flags
	// Note: Exit code 2 is reserved by Go's standard flag package for flag parsing/syntax errors.
	ExitPathError      = 3  // Workspace path not found or not a directory
	ExitPatternError   = 4  // Invalid glob pattern syntax
	ExitOutputError    = 5  // Cannot create or write to output file
	ExitNoFilesFound   = 6  // No files matching the pattern found
	ExitSearchNotFound = 7  // Search pattern not found in any matching file
	ExitMutoolExecFail = 8  // mutool binary failed execution test
	ExitMutoolNotFound = 9  // mutool binary not found in any configured location
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
	Format      string        // Output format: "json", "ndjson", or "tsv"
	Output      string        // Path to output file
	MutoolBin   string        // Optional explicit path to mutool binary
	Timeout     time.Duration // Per-file timeout for mutool execution
	Workers     int           // Number of concurrent worker goroutines
	Detect      bool          // Dry-run mode: validate prerequisites without processing

	// Sanitized paths (populated by validateConfig)
	cleanPath   string // Sanitized workspace path
	cleanOutput string // Sanitized output path
}

// main is the application entry point.
// Parses flags, handles -version, and delegates to run() for core logic.
// Exits with appropriate code based on run() result.
func main() {
	cfg, showVersion := parseFlags()
	if showVersion {
		fmt.Printf("go-pdf-extractor version %s\n", version)
		os.Exit(0)
	}
	var exitCode int
	var err error
	if cfg.Detect {
		exitCode, err = runDetect(cfg)
	} else {
		exitCode, err = run(cfg)
	}
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
	if exitCode, err := validateAndMapConfig(&cfg); err != nil {
		return exitCode, err
	}

	// Phase 2: Locate and validate mutool binary
	// Checks: -mutool-bin flag -> MUTOOL_BIN env -> PATH lookup
	mutoolPath, err := findMutool(cfg.MutoolBin)
	if err != nil {
		return ExitMutoolNotFound, err
	}

	// Phase 2b: Verify mutool executes successfully
	// Catches broken installations before wasting time on file processing
	if err := testMutoolExecution(mutoolPath); err != nil {
		return ExitMutoolExecFail, fmt.Errorf("mutool execution test failed: %v", err)
	}

	// Phase 3: Discover files matching the glob pattern (use pre-sanitized path)
	files, err := findFiles(cfg.cleanPath, cfg.FilePattern)
	if err != nil {
		return ExitPatternError, err
	}
	if len(files) == 0 {
		return ExitNoFilesFound, fmt.Errorf("no files matching pattern '%s' in %s", cfg.FilePattern, cfg.cleanPath)
	}

	// Phase 4: Process all files concurrently via worker pool
	results := processFiles(files, mutoolPath, cfg.Search, cfg.Timeout, cfg.Workers)

	// Phase 5: Write results to output file (use pre-sanitized path)
	if err := writeOutput(results, cfg.Format, cfg.cleanOutput); err != nil {
		return ExitOutputError, fmt.Errorf("writing output: %w", err)
	}

	// Phase 6: Check for partial failures (some files had errors)
	// Output is still written; exit code signals that review may be needed
	var failedFiles []string
	for _, r := range results {
		if r.Error != "" {
			failedFiles = append(failedFiles, r.Filename)
		}
	}
	if len(failedFiles) > 0 {
		return ExitPartialFailure, fmt.Errorf("%d file(s) failed: %s", len(failedFiles), strings.Join(failedFiles, ", "))
	}

	return ExitSuccess, nil
}

// runDetect executes prerequisite validation without processing files.
// Checks: path readability, file pattern matches, search pattern presence,
// output writeability, and mutool binary availability and execution.
// Returns (exitCode, error) with appropriate exit code for each failure type.
func runDetect(cfg Config) (int, error) {
	// Validate required flags first (reuse validateConfig logic)
	if exitCode, err := validateAndMapConfig(&cfg); err != nil {
		return exitCode, fmt.Errorf("[exit %d] %v", exitCode, err)
	}

	fmt.Println("Running prerequisite detection...")

	// Check 1: Validate path is readable (use pre-sanitized path from validateConfig)
	entries, err := os.ReadDir(cfg.cleanPath)
	if err != nil {
		return ExitPathError, fmt.Errorf("[exit %d] path not readable: %v", ExitPathError, err)
	}
	fmt.Printf("  [OK] Path readable: %s (%d entries)\n", cfg.cleanPath, len(entries))

	// Check 2: Validate file pattern matches files
	files, err := findFiles(cfg.cleanPath, cfg.FilePattern)
	if err != nil {
		return ExitPatternError, fmt.Errorf("[exit %d] invalid file pattern: %v", ExitPatternError, err)
	}
	if len(files) == 0 {
		return ExitNoFilesFound, fmt.Errorf("[exit %d] no files matching pattern '%s' in %s", ExitNoFilesFound, cfg.FilePattern, cfg.cleanPath)
	}
	fmt.Printf("  [OK] File pattern matches: %d file(s)\n", len(files))

	// Check 3: Locate and validate mutool binary
	mutoolPath, err := findMutool(cfg.MutoolBin)
	if err != nil {
		return ExitMutoolNotFound, fmt.Errorf("[exit %d] %v", ExitMutoolNotFound, err)
	}
	fmt.Printf("  [OK] Mutool found: %s\n", mutoolPath)

	// Check 4: Test mutool execution with version command
	if err := testMutoolExecution(mutoolPath); err != nil {
		return ExitMutoolExecFail, fmt.Errorf("[exit %d] mutool execution test failed: %v", ExitMutoolExecFail, err)
	}
	fmt.Println("  [OK] Mutool executes successfully")

	// Check 5: Validate search pattern is found in at least one file
	found, checkedCount, err := detectSearchPattern(files, mutoolPath, cfg.Search, cfg.Timeout)
	if err != nil {
		return ExitPatternError, fmt.Errorf("[exit %d] search pattern detection error: %v", ExitPatternError, err)
	}
	if !found {
		return ExitSearchNotFound, fmt.Errorf("[exit %d] search pattern '%s' not found in any of %d file(s)", ExitSearchNotFound, cfg.Search, checkedCount)
	}
	fmt.Printf("  [OK] Search pattern '%s' found in files\n", cfg.Search)

	// Check 6: Validate output path is writable
	if err := testOutputWritable(cfg.Output); err != nil {
		return ExitOutputError, fmt.Errorf("[exit %d] output not writable: %v", ExitOutputError, err)
	}
	fmt.Printf("  [OK] Output writable: %s\n", cfg.Output)

	fmt.Println("All prerequisite checks passed.")
	return ExitSuccess, nil
}

// testMutoolExecution verifies that mutool can be executed successfully.
// Runs "mutool -v" and checks for successful exit.
func testMutoolExecution(mutoolPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// #nosec G204 -- mutoolPath is sanitized and validated by findMutool()
	// not remediated: mutoolPath must be dynamic; inputs are pre-validated by findMutool()
	cmd := exec.CommandContext(ctx, mutoolPath, "-v") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("execution failed: %v (output: %s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// detectSearchPattern checks if the search pattern exists in any of the provided files.
// Processes files sequentially until a match is found (early exit on success).
// Returns (found, filesChecked, error). If all files fail processing, returns an error.
func detectSearchPattern(files []string, mutoolPath, search string, timeout time.Duration) (bool, int, error) {
	failedCount := 0
	for i, file := range files {
		result := processFile(file, mutoolPath, search, timeout)
		if result.Error != "" {
			failedCount++
			continue // Skip files that fail to process
		}

		if result.Value != nil {
			return true, i + 1, nil
		}
	}

	// If all files failed to process, report an error
	if failedCount == len(files) && len(files) > 0 {
		return false, len(files), fmt.Errorf("all %d file(s) failed to process", failedCount)
	}

	return false, len(files), nil
}

// testOutputWritable verifies that the output path can be written to.
// Creates a test file, writes data, and removes it.
func testOutputWritable(outputPath string) error {
	cleanPath, err := sanitizePath(outputPath)
	if err != nil {
		return fmt.Errorf("invalid path: %v", err)
	}

	// Check parent directory exists
	parentDir := filepath.Dir(cleanPath)
	if _, err := os.Stat(parentDir); err != nil {
		return fmt.Errorf("parent directory not accessible: %v", err)
	}

	// Create test file with unique suffix to avoid conflicts
	testPath := cleanPath + ".detect-test"
	// #nosec G304 -- cleanPath is sanitized by sanitizePath() above
	file, err := os.Create(testPath)
	if err != nil {
		return fmt.Errorf("cannot create file: %v", err)
	}

	// Write test data
	_, writeErr := file.WriteString("detect-test")
	closeErr := file.Close()

	// Clean up test file
	removeErr := os.Remove(testPath)

	// Report first error encountered
	if writeErr != nil {
		return fmt.Errorf("cannot write to file: %v", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("cannot close file: %v", closeErr)
	}
	if removeErr != nil {
		return fmt.Errorf("cannot remove test file: %v", removeErr)
	}

	return nil
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
	flag.StringVar(&cfg.Format, "format", "json", "Output format: json, ndjson, or tsv")
	flag.StringVar(&cfg.Output, "output", "", "Output file path")

	// Optional flags - have sensible defaults or are auto-detected
	flag.StringVar(&cfg.MutoolBin, "mutool-bin", "", "Path to mutool binary (optional)")
	timeout := flag.Duration("timeout", defaultTimeout, "Timeout for each mutool invocation")
	workers := flag.Int("workers", 0, "Number of worker goroutines (default: NumCPU*2, min: 2, max: 16)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	detect := flag.Bool("detect", false, "Dry-run mode: validate all prerequisites without processing")

	flag.Parse()

	cfg.Timeout = *timeout
	cfg.Workers = *workers
	cfg.Detect = *detect
	return cfg, *showVersion
}

// validateConfig checks that all required configuration is present and valid.
// Performs path sanitization and validates that workspace exists and is a directory.
// Stores sanitized paths in cfg.cleanPath and cfg.cleanOutput for reuse (DRY).
// Returns descriptive error messages for user feedback.
func validateConfig(cfg *Config) error {
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
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	if cfg.Format == "" {
		cfg.Format = "json"
	}
	if cfg.Format != "json" && cfg.Format != "ndjson" && cfg.Format != "tsv" {
		return fmt.Errorf("invalid format: %s (must be 'json', 'ndjson', or 'tsv')", cfg.Format)
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
	cfg.cleanPath = cleanPath

	// Sanitize output path (parent directory existence checked at write time)
	cleanOutput, err := sanitizePath(cfg.Output)
	if err != nil {
		return fmt.Errorf("output path error: %v", err)
	}
	cfg.cleanOutput = cleanOutput

	return nil
}

// validateAndMapConfig validates the configuration and returns the appropriate exit code and error.
// Distinguishes workspace path errors (ExitPathError) from other configuration errors (ExitConfigError).
func validateAndMapConfig(cfg *Config) (int, error) {
	if err := validateConfig(cfg); err != nil {
		if strings.Contains(err.Error(), "workspace path") {
			return ExitPathError, err
		}
		return ExitConfigError, err
	}
	return ExitSuccess, nil
}

// sanitizePath cleans and validates a filesystem path to prevent path traversal attacks.
// SECURITY: All user-supplied paths must pass through this function before use.
//
// Validation rules:
//   - Path must be absolute (no relative paths)
//   - No path traversal components (..)
//   - No null bytes or control characters
//   - No bare roots (/, C:\, D:\)
//   - No system directories (/etc, /usr, /bin, /sbin, /boot, /sys, /proc) unless bypassed
//   - Minimum depth of 2 levels required
func sanitizePath(path string) (string, error) {
	return sanitizePathExt(path, false)
}

// sanitizeExecutablePath cleans and validates a filesystem path for executables.
// It allows system directories like /bin or /usr, while rejecting other invalid paths.
func sanitizeExecutablePath(path string) (string, error) {
	return sanitizePathExt(path, true)
}

// sanitizePathExt contains the unified sanitization logic for both files and executables.
func sanitizePathExt(path string, allowSystemDirs bool) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Reject path traversal attempts before any processing
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}

	// Reject null bytes and control characters (ASCII 0-31)
	for _, r := range path {
		if r < 32 {
			return "", fmt.Errorf("path contains invalid characters")
		}
	}

	// Clean the path to normalize separators
	cleaned := filepath.Clean(path)

	// Require absolute paths only
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path must be absolute")
	}

	// Convert to absolute path for consistent handling
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
	}

	// Validate path is not a forbidden location
	if err := validatePathSecurityExt(absPath, allowSystemDirs); err != nil {
		return "", err
	}

	return absPath, nil
}

func validatePathSecurityExt(absPath string, allowSystemDirs bool) error {
	return validatePathSecurityOS(absPath, runtime.GOOS, allowSystemDirs)
}

// validatePathSecurityOS checks that a path is not a root directory on the specified OS.
// If allowSystemDirs is false, it also rejects standard system directories (like /bin or /usr).
func validatePathSecurityOS(absPath string, goos string, allowSystemDirs bool) error {
	// Normalize for comparison
	normalized := filepath.Clean(absPath)

	if goos == "windows" {
		// Windows: handle both drive paths (C:\...) and UNC paths (\\server\share\...)
		normalizedWin := strings.ReplaceAll(normalized, "/", "\\")
		if strings.HasPrefix(normalizedWin, `\\`) {
			// UNC path: \\server\share\dir\file - need at least 4 segments
			parts := strings.Split(normalizedWin, `\`)
			nonEmpty := 0
			for _, p := range parts {
				if p != "" {
					nonEmpty++
				}
			}
			// UNC needs: server, share, dir, file (4 segments minimum)
			if nonEmpty < 4 {
				return fmt.Errorf("UNC paths must have at least server, share, directory, and file")
			}
			// SECURITY: Block Windows administrative shares (C$, D$, ADMIN$, IPC$, etc.)
			// These provide access to system roots and bypass normal path restrictions
			if nonEmpty >= 2 {
				share := parts[3] // parts[0] and parts[1] are empty from leading \\
				if strings.HasSuffix(strings.ToUpper(share), "$") {
					return fmt.Errorf("administrative shares are not allowed")
				}
			}
		} else if len(normalizedWin) >= 3 && normalizedWin[1] == ':' {
			// Drive path: C:\dir\file - need at least 2 segments after drive
			parts := strings.Split(normalizedWin[3:], `\`)
			nonEmpty := 0
			for _, p := range parts {
				if p != "" {
					nonEmpty++
				}
			}
			if nonEmpty < 2 {
				return fmt.Errorf("files in root directory are not allowed")
			}
		} else {
			return fmt.Errorf("invalid Windows path format")
		}
	} else {
		// Unix: block bare root and files directly in root
		normalizedUnix := strings.ReplaceAll(normalized, "\\", "/")
		if strings.HasPrefix(normalizedUnix, "//") {
			normalizedUnix = normalizedUnix[1:]
		}
		if normalizedUnix == "/" {
			return fmt.Errorf("root paths are not allowed")
		}

		// Must have at least 2 path segments (e.g., /dir/file, not /file)
		parts := strings.Split(normalizedUnix, "/")
		nonEmpty := 0
		for _, p := range parts {
			if p != "" {
				nonEmpty++
			}
		}
		if nonEmpty < 2 {
			return fmt.Errorf("files in root directory are not allowed")
		}

		// Block system directories if not allowed
		if !allowSystemDirs {
			systemDirs := []string{"/etc", "/usr", "/bin", "/sbin", "/boot", "/sys", "/proc"}
			for _, sysDir := range systemDirs {
				if normalizedUnix == sysDir || strings.HasPrefix(normalizedUnix, sysDir+"/") {
					return fmt.Errorf("system directory %s is not allowed", sysDir)
				}
			}
		}
	}

	return nil
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
	cleanPath, err := sanitizeExecutablePath(path)
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
// SECURITY: Validates that pattern does not escape the workspace directory.
func findFiles(basePath, pattern string) ([]string, error) {
	// Reject patterns that attempt path traversal
	if strings.Contains(pattern, "..") {
		return nil, fmt.Errorf("invalid pattern: path traversal not allowed")
	}

	fullPattern := filepath.Join(basePath, pattern)

	// Verify the resolved pattern is still under basePath
	cleanBase := filepath.Clean(basePath)
	cleanPattern := filepath.Clean(fullPattern)
	if !strings.HasPrefix(cleanPattern, cleanBase+string(filepath.Separator)) && cleanPattern != cleanBase {
		return nil, fmt.Errorf("invalid pattern: escapes workspace directory")
	}

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
				// Recover from panic per-file so one bad file doesn't kill the worker
				func() {
					defer func() {
						if r := recover(); r != nil {
							// Send error result for panicked file instead of dropping it
							results <- Result{
								Filename: filepath.Base(file),
								Error:    fmt.Sprintf("panic: %v", r),
							}
						}
					}()
					result := processFile(file, mutoolPath, search, timeout)
					results <- result
				}()
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
	// not remediated: mutoolPath must be dynamic; inputs are pre-validated by findMutool()
	cmd := exec.CommandContext(ctx, mutoolPath, "draw", "-q", "-F", "txt", "-o", "-", filePath) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command

	// Configure process group for clean termination (platform-specific)
	setupProcessGroup(cmd)

	// Execute mutool and capture stdout
	output, err := cmd.Output()
	// CRITICAL: Kill process group on any error or timeout to prevent orphaned child processes.
	// exec.CommandContext only kills the main process; child processes in the group may survive.
	// This must be called even after cmd.Output() returns, as children may still be running.
	if err != nil {
		killErr := killProcessGroup(cmd)
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "timeout exceeded"
		} else {
			result.Error = fmt.Sprintf("mutool error: %v", err)
		}
		if killErr != nil {
			result.Error = fmt.Sprintf("%s; cleanup error: %v", result.Error, killErr)
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

	// Normalize CRLF to LF for consistent splitting across platforms
	normalizedText := strings.ReplaceAll(text, "\r\n", "\n")
	normalizedText = strings.ReplaceAll(normalizedText, "\r", "\n")

	for _, line := range strings.Split(normalizedText, "\n") {
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
// SECURITY: outputPath must be pre-sanitized by validateConfig().
// SECURITY: Uses atomic temp file + rename to prevent TOCTOU symlink attacks.
func writeOutput(results []Result, format, outputPath string) error {
	// SECURITY: Write to temp file then atomic rename to prevent TOCTOU attacks.
	// This avoids the race between checking symlink status and opening the file.
	outputDir := filepath.Dir(outputPath)
	tempFile, err := os.CreateTemp(outputDir, ".go-pdf-extractor-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %v", err)
	}
	tempPath := tempFile.Name()

	// Clean up temp file on any error
	success := false
	defer func() {
		if !success {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
		}
	}()

	// Use buffered writer for efficient I/O
	writer := bufio.NewWriter(tempFile)

	// Delegate to format-specific writer
	var writeErr error
	switch format {
	case "json":
		writeErr = writeJSON(writer, results)
	case "ndjson":
		writeErr = writeNDJSON(writer, results)
	case "tsv":
		writeErr = writeTSV(writer, results)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if writeErr != nil {
		return writeErr
	}

	// Flush buffer to file
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush error: %v", err)
	}

	// Close temp file before rename (required on Windows)
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close error: %v", err)
	}

	// SECURITY: Check target is not a symlink before atomic rename
	if info, err := os.Lstat(outputPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path is a symlink: refusing to follow")
		}
		// Windows: os.Rename doesn't atomically replace existing files, must remove first
		if runtime.GOOS == "windows" {
			if err := os.Remove(outputPath); err != nil {
				return fmt.Errorf("cannot remove existing output file: %v", err)
			}
		}
	}
	// Note: if Lstat fails (file doesn't exist), that's fine - rename will create it

	// Atomic rename - this is the commit point
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("cannot write output file: %v", err)
	}

	success = true
	return nil
}

// writeJSON writes results as a standard JSON array of objects.
func writeJSON(writer *bufio.Writer, results []Result) error {
	enc := json.NewEncoder(writer)
	if err := enc.Encode(results); err != nil {
		return fmt.Errorf("JSON encode error: %v", err)
	}
	return nil
}

// writeNDJSON writes results as NDJSON (newline-delimited JSON).
// Each result is a complete JSON object on its own line.
// This format is streaming-friendly and compatible with tools like jq.
func writeNDJSON(writer *bufio.Writer, results []Result) error {
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
// Multiple values are pipe-separated (|) in the value column to avoid
// ambiguity with commas that may appear in extracted values.
// Errors are not included in TSV format (value column is empty).
// Tabs and newlines in values are replaced with spaces for TSV compatibility.
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
			// Escape pipe chars in values, then join with pipe delimiter
			escaped := make([]string, len(v))
			for i, s := range v {
				escaped[i] = strings.ReplaceAll(s, "|", "\\|")
			}
			value = strings.Join(escaped, "|")
		}
		// nil values result in empty string (no special handling needed)

		// Sanitize tabs and newlines to prevent TSV corruption
		filename := sanitizeTSV(result.Filename)
		value = sanitizeTSV(value)

		line := fmt.Sprintf("%s\t%s\n", filename, value)
		if _, err := writer.WriteString(line); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
	}
	return nil
}

// sanitizeTSV removes tab and newline characters from a string for TSV output.
// Standard TSV has no escape mechanism, so we replace problematic characters
// with spaces to maintain data integrity while ensuring TSV compatibility.
func sanitizeTSV(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
