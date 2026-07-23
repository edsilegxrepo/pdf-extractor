// Package extractor provides PDF text extraction using MuPDF's mutool binary.
//
// This package exposes a library API for extracting delimiter-based values from
// PDF files. It can be imported by other Go projects or used via the CLI wrapper.
package extractor

import "time"

// Result represents the extraction outcome for a single PDF file.
type Result struct {
	Filename string      `json:"filename"`        // Base name of the processed PDF
	Value    interface{} `json:"value"`           // Extracted value(s): string, []string, or nil
	Error    string      `json:"error,omitempty"` // Error message if processing failed
}

// Options configures the extraction process.
type Options struct {
	// Path is the workspace directory containing PDF files.
	Path string

	// FilePattern is the glob pattern for file selection (e.g., "*.pdf").
	FilePattern string

	// Search is the delimiter pattern to search for (e.g., "DSFN:").
	Search string

	// MutoolBin is an optional explicit path to the mutool binary.
	// If empty, searches MUTOOL_BIN env var, then system PATH.
	MutoolBin string

	// Timeout is the per-file timeout for mutool execution.
	// Defaults to 30 seconds if zero.
	Timeout time.Duration

	// Workers is the number of concurrent worker goroutines.
	// Defaults to NumCPU*2 if zero. Bounded to [2, 16].
	Workers int
}

// DefaultTimeout is the per-file timeout for mutool execution.
const DefaultTimeout = 30 * time.Second
