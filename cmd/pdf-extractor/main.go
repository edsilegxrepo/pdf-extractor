// Package main provides the CLI for pdf-extractor.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"criticalsys.net/pdf-extractor/pkg/extractor"
)

var version = "dev"

const (
	ExitSuccess        = 0
	ExitConfigError    = 1
	ExitPathError      = 3
	ExitPatternError   = 4
	ExitOutputError    = 5
	ExitNoFilesFound   = 6
	ExitSearchNotFound = 7
	ExitMutoolExecFail = 8
	ExitMutoolNotFound = 9
	ExitPartialFailure = 10
)

func main() {
	cfg, showVersion := parseFlags()
	if showVersion {
		fmt.Printf("pdf-extractor version %s\n", version)
		os.Exit(0)
	}

	var exitCode int
	var err error
	if cfg.detect {
		exitCode, err = runDetect(cfg)
	} else {
		exitCode, err = run(cfg)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	os.Exit(exitCode)
}

type cliConfig struct {
	path        string
	filePattern string
	search      string
	format      string
	output      string
	mutoolBin   string
	timeout     time.Duration
	workers     int
	detect      bool
}

func parseFlags() (cliConfig, bool) {
	cfg := cliConfig{}

	flag.StringVar(&cfg.path, "path", "", "Workspace directory containing PDF files")
	flag.StringVar(&cfg.filePattern, "file-pattern", "", "Glob pattern for PDF files (e.g., *.pdf)")
	flag.StringVar(&cfg.search, "search", "", "Delimiter pattern to search for (e.g., DSFN:)")
	flag.StringVar(&cfg.format, "format", "json", "Output format: json, ndjson, or tsv")
	flag.StringVar(&cfg.output, "output", "", "Output file path")
	flag.StringVar(&cfg.mutoolBin, "mutool-bin", "", "Path to mutool binary (optional)")
	timeout := flag.Duration("timeout", 30*time.Second, "Timeout for each mutool invocation")
	workers := flag.Int("workers", 0, "Number of worker goroutines (default: NumCPU*2, min: 2, max: 16)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	detect := flag.Bool("detect", false, "Dry-run mode: validate all prerequisites without processing")

	flag.Parse()

	cfg.timeout = *timeout
	cfg.workers = *workers
	cfg.detect = *detect
	return cfg, *showVersion
}

func run(cfg cliConfig) (int, error) {
	if err := validateFlags(cfg); err != nil {
		return ExitConfigError, err
	}

	opts := extractor.Options{
		Path:        cfg.path,
		FilePattern: cfg.filePattern,
		Search:      cfg.search,
		MutoolBin:   cfg.mutoolBin,
		Timeout:     cfg.timeout,
		Workers:     cfg.workers,
	}

	results, err := extractor.Extract(context.Background(), opts)
	if err != nil {
		return mapError(err), err
	}

	cleanOutput, err := extractor.SanitizePath(cfg.output)
	if err != nil {
		return ExitOutputError, fmt.Errorf("output path error: %v", err)
	}

	if err := writeOutput(results, cfg.format, cleanOutput); err != nil {
		return ExitOutputError, fmt.Errorf("writing output: %w", err)
	}

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

func runDetect(cfg cliConfig) (int, error) {
	if err := validateFlags(cfg); err != nil {
		return ExitConfigError, fmt.Errorf("[exit %d] %v", ExitConfigError, err)
	}

	fmt.Println("Running prerequisite detection...")

	cleanPath, err := extractor.SanitizePath(cfg.path)
	if err != nil {
		return ExitPathError, fmt.Errorf("[exit %d] workspace path error: %v", ExitPathError, err)
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return ExitPathError, fmt.Errorf("[exit %d] path not readable: %v", ExitPathError, err)
	}
	fmt.Printf("  [OK] Path readable: %s (%d entries)\n", cleanPath, len(entries))

	files, err := extractor.FindFiles(cleanPath, cfg.filePattern)
	if err != nil {
		return ExitPatternError, fmt.Errorf("[exit %d] invalid file pattern: %v", ExitPatternError, err)
	}
	if len(files) == 0 {
		return ExitNoFilesFound, fmt.Errorf("[exit %d] no files matching pattern '%s' in %s", ExitNoFilesFound, cfg.filePattern, cleanPath)
	}
	fmt.Printf("  [OK] File pattern matches: %d file(s)\n", len(files))

	mutoolPath, err := extractor.FindMutool(cfg.mutoolBin)
	if err != nil {
		return ExitMutoolNotFound, fmt.Errorf("[exit %d] %v", ExitMutoolNotFound, err)
	}
	fmt.Printf("  [OK] Mutool found: %s\n", mutoolPath)

	if err := extractor.TestMutoolExecution(mutoolPath); err != nil {
		return ExitMutoolExecFail, fmt.Errorf("[exit %d] mutool execution test failed: %v", ExitMutoolExecFail, err)
	}
	fmt.Println("  [OK] Mutool executes successfully")

	// Check search pattern - process first few files
	opts := extractor.Options{
		Path:        cfg.path,
		FilePattern: cfg.filePattern,
		Search:      cfg.search,
		MutoolBin:   cfg.mutoolBin,
		Timeout:     cfg.timeout,
		Workers:     1,
	}
	results, err := extractor.Extract(context.Background(), opts)
	if err != nil {
		return ExitPatternError, fmt.Errorf("[exit %d] search pattern detection error: %v", ExitPatternError, err)
	}
	found := false
	for _, r := range results {
		if r.Value != nil {
			found = true
			break
		}
	}
	if !found {
		return ExitSearchNotFound, fmt.Errorf("[exit %d] search pattern '%s' not found in any of %d file(s)", ExitSearchNotFound, cfg.search, len(files))
	}
	fmt.Printf("  [OK] Search pattern '%s' found in files\n", cfg.search)

	if err := testOutputWritable(cfg.output); err != nil {
		return ExitOutputError, fmt.Errorf("[exit %d] output not writable: %v", ExitOutputError, err)
	}
	fmt.Printf("  [OK] Output writable: %s\n", cfg.output)

	fmt.Println("All prerequisite checks passed.")
	return ExitSuccess, nil
}

func validateFlags(cfg cliConfig) error {
	if cfg.path == "" {
		return fmt.Errorf("missing required flag: -path")
	}
	if cfg.filePattern == "" {
		return fmt.Errorf("missing required flag: -file-pattern")
	}
	if cfg.search == "" {
		return fmt.Errorf("missing required flag: -search")
	}
	cfg.format = strings.ToLower(strings.TrimSpace(cfg.format))
	if cfg.format == "" {
		cfg.format = "json"
	}
	if cfg.format != "json" && cfg.format != "ndjson" && cfg.format != "tsv" {
		return fmt.Errorf("invalid format: %s (must be 'json', 'ndjson', or 'tsv')", cfg.format)
	}
	if cfg.output == "" {
		return fmt.Errorf("missing required flag: -output")
	}
	return nil
}

func mapError(err error) int {
	msg := err.Error()
	if strings.Contains(msg, "workspace path") {
		return ExitPathError
	}
	if strings.Contains(msg, "no files matching") {
		return ExitNoFilesFound
	}
	if strings.Contains(msg, "mutool not found") {
		return ExitMutoolNotFound
	}
	if strings.Contains(msg, "mutool execution") {
		return ExitMutoolExecFail
	}
	if strings.Contains(msg, "pattern") {
		return ExitPatternError
	}
	return ExitConfigError
}

func testOutputWritable(outputPath string) error {
	cleanPath, err := extractor.SanitizePath(outputPath)
	if err != nil {
		return fmt.Errorf("invalid path: %v", err)
	}

	parentDir := filepath.Dir(cleanPath)
	if _, err := os.Stat(parentDir); err != nil {
		return fmt.Errorf("parent directory not accessible: %v", err)
	}

	testPath := cleanPath + ".detect-test"
	// #nosec G304 -- cleanPath is sanitized by extractor.SanitizePath() above
	file, err := os.Create(testPath)
	if err != nil {
		return fmt.Errorf("cannot create file: %v", err)
	}

	_, writeErr := file.WriteString("detect-test")
	closeErr := file.Close()
	removeErr := os.Remove(testPath)

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

func writeOutput(results []extractor.Result, format, outputPath string) error {
	outputDir := filepath.Dir(outputPath)
	tempFile, err := os.CreateTemp(outputDir, ".pdf-extractor-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %v", err)
	}
	tempPath := tempFile.Name()

	success := false
	defer func() {
		if !success {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
		}
	}()

	writer := bufio.NewWriter(tempFile)

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

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush error: %v", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close error: %v", err)
	}

	if info, err := os.Lstat(outputPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path is a symlink: refusing to follow")
		}
		if runtime.GOOS == "windows" {
			if err := os.Remove(outputPath); err != nil {
				return fmt.Errorf("cannot remove existing output file: %v", err)
			}
		}
	}

	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("cannot write output file: %v", err)
	}

	success = true
	return nil
}

func writeJSON(writer *bufio.Writer, results []extractor.Result) error {
	enc := json.NewEncoder(writer)
	if err := enc.Encode(results); err != nil {
		return fmt.Errorf("JSON encode error: %v", err)
	}
	return nil
}

func writeNDJSON(writer *bufio.Writer, results []extractor.Result) error {
	for _, result := range results {
		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("JSON marshal error: %v", err)
		}
		if _, err := writer.Write(data); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
	}
	return nil
}

func writeTSV(writer *bufio.Writer, results []extractor.Result) error {
	if _, err := writer.WriteString("filename\tvalue\n"); err != nil {
		return fmt.Errorf("write error: %v", err)
	}

	for _, result := range results {
		var value string
		switch v := result.Value.(type) {
		case string:
			value = v
		case []string:
			escaped := make([]string, len(v))
			for i, s := range v {
				escaped[i] = strings.ReplaceAll(s, "|", "\\|")
			}
			value = strings.Join(escaped, "|")
		}

		filename := sanitizeTSV(result.Filename)
		value = sanitizeTSV(value)

		line := fmt.Sprintf("%s\t%s\n", filename, value)
		if _, err := writer.WriteString(line); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
	}
	return nil
}

func sanitizeTSV(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
