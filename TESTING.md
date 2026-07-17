# go-pdf-extract Test Plan

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

## 4. CLI Tests

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

## 5. End-to-End Tests

### GoAnywhere Simulation
- Full workflow using real PDFs from `testfiles/*.pdf`
- Invoke binary with all required flags
- Verify output file is created and contains expected data

### Output File Verification
| Format | Verification |
|--------|--------------|
| JSON | Valid NDJSON, each line parses correctly |
| TSV | Valid TSV with header, correct column count |

### Test Data Location
```
testfiles/*.pdf
```

### Manual Test Command
```bash
mutool draw -q -F txt -o - testfiles/*.pdf | grep 'DSFN:'
```

## 6. Implemented Tests

Total: **94 tests** covering all functionality.

### Unit Tests (always run)

| Test | Coverage |
|------|----------|
| `TestExtractValues` | Pattern matching: single/multiple matches, spaces, deduplication, different delimiters |
| `TestValidateConfig` | All required flags, invalid format, non-existent path, path is file not directory |
| `TestFindMutool` | Flag path, env path, precedence (flag > env > PATH), not found errors |
| `TestFindFiles` | Glob matching, no matches |
| `TestFindFilesInvalidPattern` | Invalid glob pattern error handling |
| `TestWriteJSON` | JSON serialization |
| `TestWriteTSV` | TSV serialization with header |
| `TestResultSerialization` | Single value, null value, multiple values, error case |
| `TestProcessFileTimeout` | Timeout and error handling |
| `TestProcessFilesEmpty` | Empty file list input |
| `TestWriteOutputEmptyResults` | Empty results for JSON and TSV |
| `TestWriteOutputInvalidPath` | Unwritable output path |
| `TestWriteOutputUnsupportedFormat` | Unsupported format error |
| `TestWriteOutputWithAllResultTypes` | JSON and TSV with mixed result types (values, arrays, errors) |
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
| `TestIntegration_BatchProcessing` | Process all testfiles PDFs concurrently | Both files processed, 2 matches found |
| `TestIntegration_JSONOutput` | Write results to JSON file | Valid NDJSON with correct line count |
| `TestIntegration_TSVOutput` | Write results to TSV file | Valid TSV with header and data rows |
| `TestIntegration_NoMatchFile` | Search for non-existent pattern | Returns nil value, no error |
| `TestIntegration_EndToEnd` | Full workflow simulation | Output file contains both sample files |
| `TestProcessFilesWorkerPool` | Concurrent processing | All results returned with filenames |
| `TestProcessFileWithMutoolError` | Process file with mutool error | Error captured in result, no crash |

### CLI Flag Combination Tests (require mutool, skip with `-short`)

| Test | Flags Tested | Expected Result |
|------|--------------|-----------------|
| `TestIntegration_FormatJSON` | `-format json` | Valid JSON output, each line parseable |
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
| `TestRun_ExitCodes/mutool_not_found` | mutool binary not found | 2 (MutoolNotFound) |
| `TestRun_ExitCodes/invalid_glob_pattern` | Invalid file pattern | 4 (PatternError) |
| `TestRun_ExitCodes/output_path_error` | Cannot write output | 5 (OutputError) |
| `TestRun_ExitSuccess` | All files processed | 0 (Success) |

### Running Tests

```bash
# Run all tests (unit + integration)
go test -v ./...

# Run only unit tests (skip integration)
go test -v -short ./...

# Run specific integration test
go test -v -run TestIntegration_EndToEnd ./...

# Run with coverage report
go test -cover ./...

# Preserve test workspace for debugging
KEEP_TEST_WORKSPACE=1 go test -v ./...
```

## 7. Code Coverage

**Total coverage: 79.5%** (approaching 80% requirement)

### Calculating Coverage

```bash
# Generate coverage report (writes to temp, auto-cleanup)
go test -coverprofile=$TEMP/coverage.out ./... && go tool cover -func=$TEMP/coverage.out && rm -f $TEMP/coverage.out

# Windows PowerShell equivalent
go test -coverprofile=$env:TEMP/coverage.out ./...; go tool cover -func=$env:TEMP/coverage.out; Remove-Item -Force $env:TEMP/coverage.out
```

| Function | Coverage |
|----------|----------|
| `findMutool` | 100.0% |
| `findFiles` | 100.0% |
| `processFiles` | 100.0% |
| `extractValues` | 100.0% |
| `setupProcessGroup` | 100.0% |
| `killProcessGroup` | 100.0% |
| `processFile` | 95.0% |
| `run` | 94.1% |
| `validateConfig` | 91.3% |
| `writeOutput` | 88.2% |
| `validateMutoolPath` | 83.3% |
| `writeTSV` | 81.8% |
| `sanitizePath` | 66.7% |
| `validateExecutable` | 66.7% |
| `writeJSON` | 66.7% |
| `main` | 0.0% (entry point, not unit testable) |
| `parseFlags` | 0.0% (entry point, not unit testable) |

Note: `main()` and `parseFlags()` are entry points that cannot be unit tested directly. All business logic is extracted into testable functions. New security functions (`sanitizePath`, `validateExecutable`, `validateMutoolPath`) have lower coverage as some branches handle edge cases (null bytes, Unix permissions) not exercised in Windows tests.

## 8. Test Workspace Requirements

### Ephemeral Workspace

Tests must use a temporary ephemeral workspace:

- **Location**: `{TEMP}/unittests/go-pdf-extractor_{YYYYMMDDhhmmss}`, Timestamped directory prevents collisions between parallel test runs
- **Cleanup**: Purged automatically after test execution by default
- **Preservation**: Set `KEEP_TEST_WORKSPACE=1` environment variable to preserve workspace for debugging. Auto-cleanup is set by default.
