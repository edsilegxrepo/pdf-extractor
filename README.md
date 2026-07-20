# go-pdf-extractor

A command-line utility for extracting delimiter-based values from PDF files using MuPDF's mutool.

## 1. Overview

### 1.1 Purpose

go-pdf-extractor processes batches of PDF files to extract text values that follow a specified delimiter pattern. The tool is designed for integration with workflow orchestration platforms such as GoAnywhere MFT to enable content-based document routing.

### 1.2 Objectives

- Extract routing identifiers from signed documents (e.g., DocuSign PDFs)
- Process multiple files concurrently for high-throughput batch operations
- Provide machine-readable output in JSON or TSV format
- Return explicit exit codes for integration with job schedulers and monitoring systems
- Operate reliably across Windows and Linux platforms

### 1.3 Key Features

- Concurrent processing with configurable worker pool (2-16 workers)
- Support for custom delimiter patterns
- NDJSON and TSV output formats
- Per-file timeout with automatic process cleanup
- Comprehensive input sanitization
- Cross-platform compatibility (Windows/Linux)

## 2. Security Assessment

### 2.1 Threat Model

The application operates as a local command-line utility processing files from a trusted workspace directory. It does not expose network services or handle authentication credentials.

### 2.2 Encryption in Transit

**Not Applicable**: The application performs local file processing only. No network communication is initiated by the application. Data transfer to/from the application is the responsibility of the invoking system (e.g., GoAnywhere MFT).

### 2.3 Secret Management

**Not Applicable**: The application does not handle secrets, API keys, or credentials. The only sensitive configuration is the filesystem paths, which are sanitized before use.

### 2.4 Authentication Configuration

**Not Applicable**: The application does not implement authentication. Access control is managed at the filesystem and operating system level.

### 2.5 Role-Based Access Control (RBAC)

**Not Applicable**: The application runs with the permissions of the invoking user or service account. No internal RBAC mechanisms exist. Recommended deployment practice:

- Run under a dedicated service account with minimal privileges
- Restrict read access to designated workspace directories
- Restrict write access to designated output directories

### 2.6 Input Sanitization

All user-supplied inputs are sanitized:

| Input | Sanitization Method |
|-------|---------------------|
| `-path` | Path security validation (see below) |
| `-output` | Path security validation (see below) |
| `-mutool-bin` | Path sanitization + executable validation |
| `MUTOOL_BIN` env | Path sanitization + executable validation |
| `-file-pattern` | Passed to `filepath.Glob()` which validates syntax; `..` blocked |

### 2.7 Path Security

All filesystem paths (`-path`, `-output`) are validated against strict security rules to prevent path traversal attacks and writes to sensitive system locations.

#### Validation Rules

| Rule | Description |
|------|-------------|
| Absolute paths only | Relative paths are rejected |
| No path traversal | Paths containing `..` are rejected |
| No control characters | ASCII 0-31 (null bytes, tabs, etc.) are rejected |
| No root-level files | Files directly in `/` or `C:\` are rejected |
| No system directories | Linux system paths are blocked for workspace/output (see below) |

#### Blocked System Directories (Linux)

| Directory | Reason |
|-----------|--------|
| `/etc` | System configuration |
| `/usr` | System binaries and libraries |
| `/bin` | Essential system binaries |
| `/sbin` | System administration binaries |
| `/boot` | Boot loader files |
| `/sys` | Kernel/system interface |
| `/proc` | Process information |

#### Path Examples

**BLOCKED paths:**

| Path | Reason |
|------|--------|
| `relative/path` | Not absolute |
| `../etc/passwd` | Path traversal |
| `/data/../etc/passwd` | Path traversal |
| `/data/file\x00.txt` | Null byte |
| `/` | Root directory |
| `/file.txt` | File in root (no subdirectory) |
| `/etc/passwd` | System directory |
| `/usr/local/bin/app` | System directory |
| `/bin/sh` | System directory |
| `C:\` | Windows root |
| `C:\file.txt` | File in Windows root |
| `\\server\C$\file.txt` | UNC admin share (C$) |
| `\\server\ADMIN$\file.txt` | UNC admin share (ADMIN$) |

**ALLOWED paths:**

| Path | Reason |
|------|--------|
| `/data/file.txt` | Absolute, 2+ segments, not system |
| `/var/mft/output.json` | `/var` is allowed |
| `/opt/app/data.csv` | `/opt` is allowed |
| `/home/user/work/file.pdf` | User directory |
| `/tmp/output.json` | Temp directory |
| `C:\data\file.txt` | Windows with subdirectory |
| `D:\mft\batch\out.json` | Windows multi-level |
| `\\server\share\file.txt` | UNC path |
| `/data/my-file_v2.pdf` | Special chars in filename OK |
| `C:\Program Files\App\data.txt` | Spaces OK |

### 2.8 Subprocess Security

- mutool binary path is validated as an executable (system directories like `/bin` and `/usr` are allowed for binaries to support standard system installations)
- Subprocesses run in isolated process groups for clean termination
- Context-based timeouts prevent runaway processes
- No shell interpretation of arguments (direct exec, not shell command)

### 2.9 Libraries and Vulnerabilities

**Go Standard Library Only**: The application uses exclusively Go standard library packages with no third-party dependencies. This minimizes supply chain risk and ensures security patches are delivered through Go runtime updates.

| Package | Security Consideration |
|---------|------------------------|
| `os/exec` | Direct execution without shell; arguments not interpolated |
| `encoding/json` | Standard JSON marshaling; no custom parsers |
| `path/filepath` | Platform-native path handling |

**Vulnerability Scanning**: The codebase passes `govulncheck` with no known vulnerabilities in dependencies.

### 2.10 Privilege Requirements

The application requires only standard user privileges:

| Operation | Required Permission |
|-----------|---------------------|
| Read PDF files | Read access to workspace directory |
| Write output file | Write access to output directory |
| Execute mutool | Execute permission on mutool binary |

**No elevated privileges required**: The application does not require root/Administrator access for normal operation.

### 2.11 Static Analysis Results

The codebase passes security static analysis with justified exceptions:

| Tool | Finding | Disposition |
|------|---------|-------------|
| gosec G703 | Path traversal via os.Stat | Path pre-sanitized by sanitizePath() |
| gosec G204 | Subprocess with variable | mutoolPath validated as executable |
| gosec G304 | File inclusion via variable | Path sanitized immediately before use |

All exceptions are documented inline with justification comments.

## 3. Code Quality Assessment

### 3.1 Linting Compliance

The codebase passes the following linters without errors:

| Linter | Configuration |
|--------|---------------|
| gofumpt | Default settings |
| golangci-lint | Default ruleset including errcheck, staticcheck, gosimple |
| gosec | Default rules with documented exceptions |

### 3.2 Error Handling

- All function return errors are checked
- Errors are wrapped with context using `fmt.Errorf("context: %w", err)`
- Deferred cleanup errors (file.Close in defer) are intentionally ignored with documentation
- Exit codes map to specific error categories for diagnostic clarity

### 3.3 Test Coverage

| Metric | Value | Requirement |
|--------|-------|-------------|
| Statement Coverage | 79.5% | Minimum 80% |
| Functional Coverage | 100% | All CLI paths tested |

See [TESTING.md](TESTING.md) for detailed test documentation.

### 3.4 Code Organization

| Principle | Implementation |
|-----------|----------------|
| Single Responsibility | Each function has one clear purpose |
| DRY | Common logic extracted to shared functions (e.g., `validateMutoolPath`) |
| Separation of Concerns | Platform-specific code isolated via build tags |
| Explicit over Implicit | All configuration via explicit flags; no hidden defaults |

### 3.5 Documentation

- Public functions include purpose documentation
- Security-sensitive functions include SECURITY comments
- Linter exceptions include justification comments
- Architecture and design documented in separate files

## 4. Command Line Interface

### 4.1 Synopsis

```
go-pdf-extractor [OPTIONS]
```

### 4.2 Required Arguments

| Flag | Type | Description |
|------|------|-------------|
| `-path` | string | Workspace directory containing PDF files to process |
| `-file-pattern` | string | Glob pattern for file selection (e.g., `*.pdf`, `invoice_*.pdf`) |
| `-search` | string | Delimiter pattern to search for in PDF content (e.g., `DSFN:`) |
| `-format` | string | Output format: `json` or `tsv` |
| `-output` | string | Path to output file (created or overwritten) |

### 4.3 Optional Arguments

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-mutool-bin` | string | Auto-detect | Explicit path to mutool binary |
| `-workers` | int | NumCPU * 2 | Number of concurrent workers (min: 2, max: 16) |
| `-timeout` | duration | 30s | Timeout for each mutool invocation |
| `-detect` | bool | false | Dry-run mode: validate prerequisites without processing |
| `-version` | bool | false | Print version and exit |

### 4.4 Environment Variables

| Variable | Description |
|----------|-------------|
| `MUTOOL_BIN` | Path to mutool binary (used if `-mutool-bin` not specified) |

### 4.5 Exit Codes

The tool returns detailed exit codes to allow orchestration platforms (e.g., GoAnywhere MFT, Jenkins) to programmatically detect and respond to failures.

| Code | Name | Description |
|------|------|-------------|
| 0 | Success | All files processed without errors |
| 1 | ConfigError | Invalid or missing required arguments |
| 2 | - | Reserved by Go's standard `flag` library for flag syntax/parsing errors |
| 3 | PathError | Workspace path does not exist or is not a directory |
| 4 | PatternError | Invalid glob pattern syntax |
| 5 | OutputError | Cannot create or write output file |
| 6 | NoFilesFound | No files matching the pattern found |
| 7 | SearchNotFound | Search pattern not found in any file (detect mode only) |
| 8 | MutoolExecFail | mutool binary failed execution test |
| 9 | MutoolNotFound | mutool binary not found or invalid |
| 10 | PartialFailure | Some files failed processing (output still written) |

### 4.6 Output Formats

#### 4.6.1 Newline-Delimited JSON (NDJSON)
When `-format json` is specified, the output file contains one JSON object per line (streaming-friendly format):

| Field | Type | Description |
|-------|------|-------------|
| `filename` | string | Base name of the processed PDF file |
| `value` | string, array, or null | Extracted value(s); null if no match found. Array if multiple matches are found in the same file. |
| `error` | string (optional) | Error message if processing failed for this specific file |

Example line:
```json
{"filename":"doc1.pdf","value":"327078_X_X_X_X_Wage.pdf"}
```

#### 4.6.2 Tab-Separated Values (TSV)
When `-format tsv` is specified, the output file contains a header row followed by tab-separated values:

*   **Format:** `filename<TAB>value<NEWLINE>`
*   **Multi-Value Separation:** Multiple values extracted from a single file are **pipe-separated (`|`)** to avoid ambiguity with commas that may appear in document text.
*   **Errors:** Errors are not included in the TSV output; the value column is left empty in case of errors or if no match is found.

Example line:
```
doc1.pdf	value1|value2
```


## 5. Usage Examples

### 5.1 Basic Extraction

Extract DSFN values from all PDFs in a workspace:

```bash
go-pdf-extractor \
  -path /data/workspace/batch001 \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /data/output/routing.json
```

**Output** (`routing.json`):
```json
{"filename":"document1.pdf","value":"327078_X_X_X_X_Wage.pdf"}
{"filename":"document2.pdf","value":"Employee ID_X_X_X_X_Eag-AHP.pdf"}
{"filename":"document3.pdf","value":null}
```

### 5.2 TSV Output for Review

Generate human-readable TSV output:

```bash
go-pdf-extractor \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format tsv \
  -output /tmp/review.tsv
```

**Output** (`review.tsv`):
```
filename	value
document1.pdf	327078_X_X_X_X_Wage.pdf
document2.pdf	Employee ID_X_X_X_X_Eag-AHP.pdf
document3.pdf	
```

### 5.3 Custom Worker Count

Limit concurrency on resource-constrained systems:

```bash
go-pdf-extractor \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /tmp/results.json \
  -workers 4
```

### 5.4 Extended Timeout for Large Files

Process large or complex PDFs:

```bash
go-pdf-extractor \
  -path /data/large_documents \
  -file-pattern "report_*.pdf" \
  -search "Reference:" \
  -format json \
  -output /tmp/references.json \
  -timeout 120s
```

### 5.5 Explicit mutool Path

Use a specific mutool installation:

```bash
go-pdf-extractor \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /tmp/results.json \
  -mutool-bin /var/opt/bin/mutool
```

### 5.6 Windows PowerShell

```powershell
.\go-pdf-extractor.exe `
  -path "D:\Data\Workspace\batch001" `
  -file-pattern "*.pdf" `
  -search "DSFN:" `
  -format json `
  -output "D:\Data\Output\routing.json"
```

### 5.7 Environment Variable Configuration

```bash
export MUTOOL_BIN=/var/opt/bin/mutool

go-pdf-extractor \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /tmp/results.json
```

### 5.8 Integration with jq

Parse JSON output for specific values:

```bash
go-pdf-extractor \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /dev/stdout | jq -r 'select(.value != null) | .filename'
```

### 5.9 Prerequisite Detection (Dry Run)

Validate all prerequisites before processing with `-detect`:

```bash
go-pdf-extractor \
  -path /data/workspace/batch001 \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /data/output/routing.json \
  -detect
```

**Output (success)**:
```
Running prerequisite detection...
  [OK] Path readable: /data/workspace/batch001 (15 entries)
  [OK] File pattern matches: 12 file(s)
  [OK] Mutool found: /usr/bin/mutool
  [OK] Mutool executes successfully
  [OK] Search pattern 'DSFN:' found in files
  [OK] Output writable: /data/output/routing.json
All prerequisite checks passed.
```

**Output (failure)**:
```
Running prerequisite detection...
  [OK] Path readable: /data/workspace/batch001 (15 entries)
  [OK] File pattern matches: 12 file(s)
Error: [exit 2] mutool not found in PATH, set MUTOOL_BIN or use -mutool-bin flag
```

The `-detect` flag is useful for:
- **Deployment validation**: Verify environment setup before scheduling batch jobs
- **CI/CD pipelines**: Pre-flight check before production runs
- **Troubleshooting**: Isolate which prerequisite is failing
- **GoAnywhere MFT**: Add a detect step before processing to catch configuration issues early

### 5.10 Error Handling in Scripts

```bash
#!/bin/bash
go-pdf-extractor \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /tmp/results.json

case $? in
  0)  echo "All files processed successfully" ;;
  1)  echo "Configuration error" >&2; exit 1 ;;
  2)  echo "CLI syntax error / reserved" >&2; exit 1 ;;
  3)  echo "Workspace path error" >&2; exit 1 ;;
  4)  echo "Invalid file pattern" >&2; exit 1 ;;
  5)  echo "Cannot write output" >&2; exit 1 ;;
  6)  echo "No files matching pattern" >&2; exit 1 ;;
  7)  echo "Search pattern not found in any file" >&2; exit 1 ;;
  8)  echo "mutool execution failed" >&2; exit 1 ;;
  9)  echo "mutool not found" >&2; exit 1 ;;
  10) echo "Some files failed; check output for errors" >&2 ;;
esac
```

## 6. Deployment

### 6.1 Prerequisites

1. **Go Runtime**: Go 1.21 or later (for building from source)
2. **mutool**: MuPDF tools package version 1.28.0 or compatible

### 6.2 Building from Source

```bash
# Clone repository
git clone https://github.com/<owner>/go-pdf-extractor
cd go-pdf-extractor

# Build for current platform (development)
go build -o ../bin/go-pdf-extractor ./...

# Build with version information (release)
go build -ldflags "-s -w -X main.version=1.0.0" -trimpath -buildmode=pie -o ../bin/go-pdf-extractor ./...

# Cross-compile for Linux
GOOS=linux

# Cross-compile for Windows
GOOS=windows
```

### 6.3 Installing mutool

**Linux (RHEL/CentOS)**:
```bash
sudo yum install epel-release
sudo yum install mupdf-tools
```

**Linux (Ubuntu/Debian)**:
```bash
sudo apt-get update
sudo apt-get install mupdf-tools
```

**Windows**:
1. Download MuPDF from https://mupdf.com/downloads/
2. Extract to desired location (e.g., `C:\Program Files\mupdf`)
3. Add to PATH or configure `MUTOOL_BIN` environment variable

### 6.4 Verification

```bash
# Verify mutool installation
mutool -v

# Verify go-pdf-extractor version
./go-pdf-extractor -version

# Verify go-pdf-extractor help
./go-pdf-extractor -h

# Test with sample file
echo "Test DSFN:12345" | mutool draw -q -F txt -o - /dev/stdin
```

### 6.5 GoAnywhere MFT Integration

1. Deploy `go-pdf-extractor` binary to accessible location on GoAnywhere server
2. Configure mutool path via `MUTOOL_BIN` environment variable or workflow parameter
3. Create workflow with Execute Command task:
   - Command: `/path/to/go-pdf-extractor`
   - Arguments: `-path "${workspace}" -file-pattern "*.pdf" -search "DSFN:" -format json -output "${workspace}/routing.json"`
   Note: use folder variables instead of hardcoded values
4. Add conditional routing based on exit code
5. Parse output file for document routing decisions

## 7. Related Documentation

- [DESIGN.md](DESIGN.md) - Design specifications and requirements
- [ARCHITECTURE.md](ARCHITECTURE.md) - Architecture decisions and data flow
- [TESTING.md](TESTING.md) - Test plan and coverage metrics
