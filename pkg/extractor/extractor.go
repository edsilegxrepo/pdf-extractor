package extractor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Extract processes PDF files matching the pattern and extracts values.
// Returns results for all matching files, including any per-file errors.
func Extract(ctx context.Context, opts Options) ([]Result, error) {
	// Apply defaults
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}

	// Validate and sanitize workspace path
	cleanPath, err := SanitizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("workspace path error: %w", err)
	}

	// Validate path is a directory
	if err := ValidateDirectory(cleanPath); err != nil {
		return nil, fmt.Errorf("workspace path error: %w", err)
	}

	// Locate and validate mutool binary
	mutoolPath, err := FindMutool(opts.MutoolBin)
	if err != nil {
		return nil, err
	}

	// Verify mutool executes successfully
	if err := TestMutoolExecution(mutoolPath); err != nil {
		return nil, fmt.Errorf("mutool execution test failed: %w", err)
	}

	// Discover files matching the glob pattern
	files, err := FindFiles(cleanPath, opts.FilePattern)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files matching pattern '%s' in %s", opts.FilePattern, cleanPath)
	}

	// Process all files concurrently via worker pool
	results := processFiles(files, mutoolPath, opts.Search, opts.Timeout, opts.Workers)

	return results, nil
}

// processFiles concurrently processes all PDF files using a bounded worker pool.
func processFiles(files []string, mutoolPath, search string, timeout time.Duration, workers int) []Result {
	// Apply worker count bounds
	numWorkers := workers
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU() * 2
	}
	if numWorkers < 2 {
		numWorkers = 2
	}
	if numWorkers > 16 {
		numWorkers = 16
	}

	jobs := make(chan string, len(files))
	results := make(chan Result, len(files))

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				func() {
					defer func() {
						if r := recover(); r != nil {
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

	for _, file := range files {
		jobs <- file
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var allResults []Result
	for result := range results {
		allResults = append(allResults, result)
	}

	return allResults
}

// processFile extracts values from a single PDF file using mutool.
func processFile(filePath, mutoolPath, search string, timeout time.Duration) Result {
	filename := filepath.Base(filePath)
	result := Result{Filename: filename}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// #nosec G204 -- mutoolPath is validated by FindMutool(); filePath comes from filepath.Glob
	cmd := exec.CommandContext(ctx, mutoolPath, "draw", "-q", "-F", "txt", "-o", "-", filePath)

	setupProcessGroup(cmd)

	output, err := cmd.Output()
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

	values := extractValues(string(output), search)

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
func extractValues(text, search string) []string {
	var values []string
	seen := make(map[string]bool)

	normalizedText := strings.ReplaceAll(text, "\r\n", "\n")
	normalizedText = strings.ReplaceAll(normalizedText, "\r", "\n")

	for _, line := range strings.Split(normalizedText, "\n") {
		idx := strings.Index(line, search)
		if idx == -1 {
			continue
		}

		value := strings.TrimSpace(line[idx+len(search):])

		if value != "" && !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}

	return values
}

// FindFiles discovers files matching the glob pattern in the workspace directory.
func FindFiles(basePath, pattern string) ([]string, error) {
	if strings.Contains(pattern, "..") {
		return nil, fmt.Errorf("invalid pattern: path traversal not allowed")
	}

	fullPattern := filepath.Join(basePath, pattern)

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
