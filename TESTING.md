# pdf-extractor Test Plan

## 1. Unit Tests

### Pattern Matching
- Validate string extraction for various `DSFN:` formats
- With/without spaces after delimiter
- Different value lengths and character types (digits, letters, delimiters)

### Output Formatting
- JSON serialization correctness
- TSV serialization correctness
- Proper escaping of special characters

### mutool Binary Detection
- Precedence order: `-mutool-bin` flag → `MUTOOL_BIN` env → PATH
- Error when binary not found in any location

## 2. Integration Tests

### Single File Processing
| Scenario | Input | Expected Output |
|----------|-------|-----------------|
| Single match | PDF with one `DSFN:` value | One result with extracted value |
| Multiple matches | PDF with multiple `DSFN:` values | All values returned |
| No match | PDF without `DSFN:` pattern | Result with null value |

### Batch Processing
| Scenario | Input | Expected Output |
|----------|-------|-----------------|
| Mixed results | Multiple PDFs with varied content | Correct results for each file |
| All matches | Multiple PDFs all containing pattern | All values extracted |
| No matches | Multiple PDFs none containing pattern | All results with null values |

### Error Handling
| Scenario | Input | Expected Output |
|----------|-------|-----------------|
| Corrupted PDF | Invalid PDF file | Skip with error logged, continue |
| Password-protected | Encrypted PDF | Skip with error logged, continue |
| Empty file | Zero-byte PDF | Skip with error logged, continue |

## 3. Concurrency Tests

### Worker Pool
- Verify goroutine count respects `runtime.NumCPU() * 2` limit
- No race conditions in result aggregation

### Large Batch Processing
- Process 50+ files correctly in parallel
- Results are complete and accurate regardless of processing order

## 4. Detect Mode Tests

### Prerequisite Validation
| Scenario | Expected Behavior |
|----------|-------------------|
| All checks pass | Exit 0, all `[OK]` messages displayed |
| Path not readable | Exit 3, error with `[exit 3]` prefix |
| No matching files | Exit 6, error with `[exit 6]` prefix |
| mutool not found | Exit 2, error with `[exit 2]` prefix |
| mutool execution fails | Exit 8, error with `[exit 8]` prefix |
| Search pattern not found | Exit 7, error with `[exit 7]` prefix |
| Output not writable | Exit 5, error with `[exit 5]` prefix |

### Detect Mode Behavior
| Scenario | Expected Behavior |
|----------|-------------------|
| `-detect` flag present | Run prerequisite checks only, no file processing |
| mutool via `-mutool-bin` | Validate specified path, test execution |
| mutool via `MUTOOL_BIN` env | Validate env path, test execution |
| mutool via PATH | Validate PATH lookup, test execution |
| Search pattern check | Process files until match found (early exit) |
| Output writeability | Create test file, write, remove |

## 5. CLI Tests

### Required Flags Validation
| Missing Flag | Expected Behavior |
|--------------|-------------------|
| `-path` | Clear error message, non-zero exit |
| `-file-pattern` | Clear error message, non-zero exit |
| `-search` | Clear error message, non-zero exit |
| `-format` | Clear error message, non-zero exit |
| `-output` | Clear error message, non-zero exit |

### Invalid Input Handling
| Scenario | Expected Behavior |
|----------|-------------------|
| Non-existent workspace | Clear error message, non-zero exit |
| Invalid format value | Clear error message listing valid options |
| No matching files | Empty output file (valid JSON/TSV structure) |
| Output path not writable | Clear error message, non-zero exit |

### mutool Availability
| Scenario | Expected Behavior |
|----------|-------------------|
| Not in PATH, no env, no flag | Clear error message, non-zero exit |
| Invalid path in flag | Clear error message, non-zero exit |

## 6. End-to-End Tests

### GoAnywhere Simulation
- Full workflow using real PDFs from `testdata/*.pdf`
- Invoke binary with all required flags
- Verify output file is created and contains expected data

### Output File Verification
| Format | Verification |
|--------|--------------|
| JSON | Valid standard JSON array of objects |
| NDJSON | Valid Newline-Delimited JSON (NDJSON) |
| TSV | Valid TSV with header, correct column count |

### Test Data Location
```
testdata/*.pdf
```

### Manual Test Command
```bash
mutool draw -q -F txt -o - testdata/*.pdf | grep 'DSFN:'
```

## 7. Test Structure

Tests are organized by package:

```
pdf-extractor/
├── cmd/pdf-extractor/
│   └── main_test.go         # CLI integration tests
├── pkg/extractor/
│   └── extractor_test.go    # Library unit + integration tests
└── testdata/               # Test PDF files
    ├── sample001.pdf        # DSFN:value (no space)
    ├── sample002.pdf        # DSFN: value (with space)
    ├── multi-match.pdf      # Multiple DSFN values
    ├── no-match.pdf         # No DSFN pattern
    ├── empty.pdf            # Blank PDF
    ├── duplicate-values.pdf # Deduplication test
    ├── whitespace-variations.pdf
    ├── unicode-value.pdf    # Special characters
    ├── long-value.pdf       # Long filename
    └── different-delimiter.pdf  # REF:, ID:, DOC_NUM:
```

### Library Tests (pkg/extractor)

| Test | Coverage |
|------|----------|
| `TestExtractValues` | Pattern matching: single/multiple matches, spaces, deduplication, line endings |
| `TestExtractValues_EdgeCases` | Empty text, empty search, unicode, special chars |
| `TestSanitizePath` | Empty path, path traversal, relative paths |
| `TestSanitizeExecutablePath` | System dirs allowed, traversal rejected |
| `TestValidatePathSecurityOS` | Unix/Windows path security rules |
| `TestFindFiles` | Glob matching, no matches, path traversal rejection |
| `TestValidateExecutable` | Directory rejection, non-existent files, Windows extension check |
| `TestValidateDirectory` | Valid dir, non-existent, file not dir |
| `TestFindMutool` | PATH lookup, explicit path, invalid path |
| `TestTestMutoolExecution` | Valid mutool, invalid binary |
| `TestExtract_SingleFile` | sample001.pdf, sample002.pdf extraction |
| `TestExtract_BatchProcessing` | Concurrent processing of multiple files |
| `TestExtract_NoMatch` | Search pattern not found |
| `TestExtract_InvalidPath` | Non-existent workspace |
| `TestExtract_NoMatchingFiles` | No files match pattern |
| `TestExtract_DefaultTimeout` | Zero timeout uses default |
| `TestExtract_Errors` | Empty path, traversal, relative path |
| `TestExtract_MultiMatch` | Multiple values in one file |
| `TestExtract_NoMatchFile` | no-match.pdf returns nil |
| `TestExtract_EmptyFile` | empty.pdf returns nil |
| `TestExtract_DuplicateValues` | Deduplication works |
| `TestExtract_WhitespaceVariations` | Trimming works |
| `TestExtract_LongValue` | Long filename extracted |
| `TestExtract_UnicodeValue` | Special chars preserved |
| `TestExtract_DifferentDelimiter` | REF:, ID:, DOC_NUM: patterns |
| `TestExtract_AllTestFiles` | All 10 PDFs processed |
| `TestProcessFiles_WorkerBounds` | min=2, max=16, default=NumCPU*2 |
| `TestProcessFiles_Empty` | Empty input returns empty |
| `TestResultTypes` | String, []string, nil values |

### CLI Integration Tests (cmd/pdf-extractor)

| Test | Coverage |
|------|----------|
| `TestCLI_JSONOutput` | JSON array format, correct values |
| `TestCLI_NDJSONOutput` | NDJSON format, line count |
| `TestCLI_TSVOutput` | TSV header + data rows |
| `TestCLI_DetectMode_Success` | All prerequisite checks pass |
| `TestCLI_DetectMode_PathNotFound` | Exit code 3 |
| `TestCLI_DetectMode_NoMatchingFiles` | Exit code 6 |
| `TestCLI_DetectMode_SearchNotFound` | Exit code 7 |
| `TestCLI_ExitCode_Success` | Exit code 0 |
| `TestCLI_ExitCode_MissingRequiredFlag` | Exit code 1 |
| `TestCLI_ExitCode_InvalidPath` | Exit code 3 |
| `TestCLI_ExitCode_NoFilesFound` | Exit code 6 |
| `TestCLI_Version` | Version output |
| `TestCLI_WorkersFlag` | Workers 1, 4, 8, 20 |
| `TestCLI_TimeoutFlag` | Custom timeout |
| `TestCLI_SpecificFilePattern` | Single file pattern |
| `TestCLI_NoMatchReturnsNullValues` | Null for no matches |

## 8. Legacy Test Reference

The following tests were in the original monolithic test file before refactoring.

### Unit Tests (always run)

| Test | Coverage |
|------|----------|
| `TestExtractValues` | Pattern matching: single/multiple matches, spaces, deduplication, different delimiters, CRLF/CR/mixed line endings, UTF-8/Unicode, nested delimiters, literal regex delimiter matching |
| `TestValidateConfig` | All required flags, invalid format, non-existent path, path is file not directory |
| `TestFindMutool` | Flag path, env path, precedence (flag > env > PATH), not found errors |
| `TestFindFiles` | Glob matching, no matches |
| `TestFindFilesInvalidPattern` | Invalid glob pattern error handling |
| `TestFindFiles_PathTraversal` | Path traversal patterns rejected (../, encoded) |
| `TestWriteJSON` | JSON serialization |
| `TestWriteTSV` | TSV serialization with header |
| `TestWriteTSV_SpecialCharacters` | TSV sanitizes tabs/newlines in values |
| `TestSanitizeTSV` | Special character replacement (tabs, newlines, CR) |
| `TestResultSerialization` | Single value, null value, multiple values, error case |
| `TestProcessFileTimeout` | Timeout and error handling |
| `TestProcessFilesEmpty` | Empty file list input |
| `TestWriteOutputEmptyResults` | Empty results for JSON and TSV |
| `TestWriteOutputInvalidPath` | Unwritable output path |
| `TestWriteOutputUnsupportedFormat` | Unsupported format error |
| `TestWriteOutputWithAllResultTypes` | JSON and TSV with mixed result types (values, arrays, errors), pipe escaping in multi-values |
| `TestRun_Success` | Successful run returns exit code 0 |
| `TestRun_InvalidConfig` | Invalid config returns error |
| `TestRun_MutoolNotFound` | Missing mutool returns ExitMutoolNotFound |
| `TestRun_InvalidGlobPattern` | Invalid pattern returns ExitPatternError |
| `TestRun_OutputWriteError` | Unwritable output returns ExitOutputError |

### Integration Tests (require mutool, skip with `-short`)

| Test | Description | Expected Result |
|------|-------------|-----------------|
| `TestIntegration_SingleFileWithMatch` | Process sample001.pdf | Extracts `Employee ID_X_X_X_X_Eag-AHP.pdf` from `DSFN:Employee ID_X_X_X_X_Eag-AHP.pdf` |
| `TestIntegration_SingleFileWithSpaceAfterDelimiter` | Process sample002.pdf | Extracts `327078_X_X_X_X_Wage.pdf` from `DSFN: 327078_X_X_X_X_Wage.pdf` |
| `TestIntegration_BatchProcessing` | Process all testdata PDFs concurrently | Both files processed, 2 matches found |
| `TestIntegration_JSONOutput` | Write results to JSON file | Valid standard JSON array |
| `TestIntegration_NDJSONOutput` | Write results to NDJSON file | Valid NDJSON with correct line count |
| `TestIntegration_TSVOutput` | Write results to TSV file | Valid TSV with header and data rows |
| `TestIntegration_NoMatchFile` | Search for non-existent pattern | Returns nil value, no error |
| `TestIntegration_EndToEnd` | Full workflow simulation | Output file contains both sample files |
| `TestProcessFilesWorkerPool` | Concurrent processing | All results returned with filenames |
| `TestProcessFileWithMutoolError` | Process file with mutool error | Error captured in result, no crash |
| `TestIntegration_MultipleMatches` | Process file with multiple matches | Returns slice of values, correctly ordered and deduplicated |

### CLI Flag Combination Tests (require mutool, skip with `-short`)

| Test | Flags Tested | Expected Result |
|------|--------------|-----------------|
| `TestIntegration_FormatJSON` | `-format json` | Valid standard JSON array |
| `TestIntegration_FormatNDJSON` | `-format ndjson` | Valid NDJSON output, each line parseable |
| `TestIntegration_FormatTSV` | `-format tsv` | Valid TSV with header row |
| `TestIntegration_DifferentSearchPattern` | `-search NONEXISTENT:` | All results have null value |
| `TestIntegration_FilePatternSpecific` | `-file-pattern sample001.pdf` | Only one file processed |
| `TestIntegration_MutoolBinFlag` | `-mutool-bin <path>` | Uses explicit mutool path |
| `TestIntegration_TimeoutFlag` | `-timeout 60s` | Completes within timeout |
| `TestIntegration_NoMatchingFiles` | `-file-pattern *.nonexistent` | Empty output file |
| `TestIntegration_AllFlagsCombined` | All flags in various combinations | Output files created for each combo |
| `TestIntegration_WorkersFlag` | `-workers` with various values | Correct results with 0, 1, 4, 20 workers |
| `TestProcessFilesWorkersBounds` | Worker pool bounds | Enforces min=2, max=16, default=NumCPU*2 |

### Exit Code Tests

| Test | Scenario | Expected Exit Code |
|------|----------|-------------------|
| `TestRun_ExitCodes/missing_required_flag` | Missing required CLI flag | 1 (ConfigError) |
| `TestRun_ExitCodes/workspace_path_not_found` | Path does not exist | 3 (PathError) |
| `TestRun_ExitCodes/mutool_not_found` | mutool binary not found | 9 (MutoolNotFound) |
| `TestRun_ExitCodes/invalid_glob_pattern` | Invalid file pattern | 4 (PatternError) |
| `TestRun_ExitCodes/output_path_error` | Cannot write output | 5 (OutputError) |
| `TestRun_ExitSuccess` | All files processed | 0 (Success) |

### Detect Mode Tests (require mutool, skip with `-short`)

| Test | Scenario | Expected Exit Code |
|------|----------|-------------------|
| `TestDetect_Success` | All prerequisites pass | 0 (Success) |
| `TestDetect_PathNotReadable` | Path does not exist or not readable | 3 (PathError) |
| `TestDetect_PathNotDirectory` | Path is a file, not a directory | 3 (PathError) |
| `TestDetect_NoFilesFound` | No files matching pattern | 6 (NoFilesFound) |
| `TestDetect_MutoolNotFound` | mutool binary not found | 9 (MutoolNotFound) |
| `TestDetect_MutoolExecFail` | mutool binary exists but fails execution | 8 (MutoolExecFail) |
| `TestDetect_SearchNotFound` | Search pattern not in any file | 7 (SearchNotFound) |
| `TestDetect_OutputNotWritable` | Output path not writable | 5 (OutputError) |
| `TestDetect_ExitCodeInErrorMessage` | Error messages include exit code | Verify `[exit N]` format |

### Detect Mode Unit Tests

| Test | Coverage |
|------|----------|
| `TestRunDetect_Success` | Full successful prerequisite detection |
| `TestRunDetect_PathNotReadable` | Non-existent path returns ExitPathError |
| `TestRunDetect_PathIsFile` | Path is file (not directory) returns ExitPathError |
| `TestRunDetect_NoFilesFound` | No matching files returns ExitNoFilesFound |
| `TestRunDetect_MutoolNotFound` | Missing mutool returns ExitMutoolNotFound |
| `TestRunDetect_OutputNotWritable` | Non-writable output returns ExitOutputError |
| `TestRunDetect_SearchNotFound` | Pattern not in files returns ExitSearchNotFound |
| `TestRunDetect_ExitCodeInErrorMessage` | Error messages contain `[exit N]` prefix |
| `TestTestMutoolExecution` | Valid mutool executes successfully |
| `TestTestMutoolExecution_Invalid` | Invalid binary returns error |
| `TestDetectSearchPattern` | Pattern found with early exit |
| `TestDetectSearchPattern_NotFound` | Pattern not found in any file |
| `TestTestOutputWritable` | Valid path passes, test file cleaned up |
| `TestTestOutputWritable_NotWritable` | Non-writable path returns error |
| `TestTestOutputWritable_InvalidPath` | Empty path returns error |

### Additional Unit Tests

| Test | Coverage |
|------|----------|
| `TestSanitizePathExt` | Unified sanitization logic (rejects traversals/nulls/empty/control characters for both data and executable paths, validates system directories bypass) |
| `TestSanitizePath_Security` | Path traversal, relative paths, control chars, roots, system dirs, UNC admin shares blocked for sanitizePath |
| `TestValidatePathSecurityOS` | Path security rules for both Windows and Unix OS targets (under both data and executable modes) |
| `TestSanitizePath_ValidPaths` | Valid absolute paths with proper depth are accepted |
| `TestValidateExecutable_Directory` | Directories are rejected as executables |
| `TestValidateExecutable_NotExists` | Non-existent paths are rejected as executables |
| `TestValidateExecutable_NotExecutable` | Non-executable files rejected (Unix only) |
| `TestWriteJSON_Success` | Successful standard JSON array writing |
| `TestWriteJSON_Error` | JSON array write error handling when the writer fails |
| `TestWriteNDJSON_Success` | Successful NDJSON writing |
| `TestWriteNDJSON_Error` | NDJSON write error handling when the writer fails |
| `TestMain_Version` | Subprocess-based test verifying version output, main, and parseFlags |
| `TestKillProcessGroup_NilProcess` | Handles nil Process field without error |
| `TestKillProcessGroup_AlreadyFinished` | Returns nil error if the process already exited (ignores expected dead-process errors) |
| `TestKillProcessGroup_Running` | Terminates a running process group without returning an error |

### Running Tests

```bash
# Run all tests (unit + integration)
go test -v ./...

# Run library package tests only
go test -v ./pkg/extractor/...

# Run only unit tests (skip integration)
go test -v -short ./...

# Run specific integration test
go test -v -run TestIntegration_EndToEnd ./...

# Run with coverage report
go test -cover ./...

# Preserve test workspace for debugging
KEEP_TEST_WORKSPACE=1 go test -v ./...

# Run tests with data race detector (requires CGO and gcc/clang compiler)
CGO_ENABLED=1 go test -race -v ./...

# Windows PowerShell race detector equivalent
$env:CGO_ENABLED="1"; go test -race -v ./...
```

#### Race Detector Prerequisites

The Go race detector uses thread instrumentation which requires **CGO** (`CGO_ENABLED=1`) and a host C compiler:
*   **Linux**: Install `build-essential` or `gcc` (`sudo apt-get install build-essential` or `sudo yum groupinstall "Development Tools"`).
*   **Windows**: Requires a Windows port of GCC such as **MinGW-w64**. It can be installed via MSYS2 or Chocolatey (`choco install mingw`). Ensure the `gcc` binary is added to your system `PATH` variable.

If the Go toolchain fails to locate your C compiler, or if multiple compilers are installed, explicitly specify the compiler path using the **`CC`** environment variable (e.g., `$env:CC="gcc"` or `$env:CC="x86_64-w64-mingw32-gcc"` in Windows PowerShell, or `export CC=gcc` in Unix shells).


## 9. Code Coverage

**Total coverage: 88.5%** (target: 80%)

### Calculating Coverage

```bash
# Generate coverage report for library package
go test -coverprofile=$TEMP/coverage.out ./pkg/extractor/... && go tool cover -func=$TEMP/coverage.out && rm -f $TEMP/coverage.out

# Generate coverage report for all packages
go test -coverprofile=$TEMP/coverage.out ./... && go tool cover -func=$TEMP/coverage.out && rm -f $TEMP/coverage.out

# Windows PowerShell equivalent
go test -coverprofile=$env:TEMP/coverage.out ./...; go tool cover -func=$env:TEMP/coverage.out; Remove-Item -Force $env:TEMP/coverage.out
```

| Function | Coverage |
|----------|----------|
| `findMutool` | 87.5% |
| `findFiles` | 90.9% |
| `processFiles` | 96.8% |
| `extractValues` | 100.0% |
| `setupProcessGroup` | 100.0% |
| `killProcessGroup` | 100.0% |
| `testMutoolExecution` | 100.0% |
| `validateMutoolPath` | 100.0% |
| `validateConfig` | 100.0% |
| `validateAndMapConfig` | 100.0% |
| `sanitizePath` | 93.8% |
| `sanitizeTSV` | 100.0% |
| `processFile` | 100.0% |
| `run` | 86.4% |
| `runDetect` | 87.1% |
| `detectSearchPattern` | 72.7% |
| `writeTSV` | 87.5% |
| `testOutputWritable` | 80.0% |
| `validateExecutable` | 75.0% |
| `writeOutput` | 71.9% |
| `writeJSON` | 77.8% |
| `validatePathSecurityExt` | 100.0% |
| `validatePathSecurityOS` | 97.6% |
| `sanitizeExecutablePath` | 100.0% |
| `sanitizePathExt` | 100.0% |
| `main` | 33.3% |
| `parseFlags` | 100.0% |

Note: `main()` and `parseFlags()` are tested via helper subprocess execution tests in the test suite to achieve validation. `validatePathSecurityExt` has 100.0% coverage by separating target OS validation into a mockable `validatePathSecurityOS` helper function.

## 10. Test Workspace Requirements

### Ephemeral Workspace

Tests must use a temporary ephemeral workspace:

- **Location**: `{TEMP}/unittests/pdf-extractor_{YYYYMMDDhhmmss}`, Timestamped directory prevents collisions between parallel test runs
- **Cleanup**: Purged automatically after test execution by default
- **Preservation**: Set `KEEP_TEST_WORKSPACE=1` environment variable to preserve workspace for debugging. Auto-cleanup is set by default.
