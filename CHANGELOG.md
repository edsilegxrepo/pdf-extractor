# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-07-22

### Added

- Library API: `pkg/extractor` package with `Extract()` function for programmatic use
- Importable types: `Options`, `Result` in `pkg/extractor`
- Exported functions: `FindMutool()`, `FindFiles()`, `SanitizePath()`, `ValidateExecutable()`

### Changed

- Refactored project structure to support both CLI and library usage
- CLI moved to `cmd/pdf-extractor/`
- Core logic moved to `pkg/extractor/`
- Platform-specific process management moved to `pkg/extractor/`

### Notes

- Zero functionality loss - all CLI behavior unchanged
- No performance impact - compile-time reorganization only
- External projects can now import `criticalsys.net/pdf-extractor/pkg/extractor`

## [0.1.0] - 2026-07-16

### Added

- Initial release
- CLI tool for extracting delimiter-based values from PDF files using MuPDF's mutool
- Concurrent worker pool for parallel PDF processing
- Output formats: JSON, NDJSON, TSV
- Dry-run mode (`-detect`) for prerequisite validation
- Path security validation (traversal prevention, system directory blocking)
- Platform-specific process group management (Windows/Unix)
- Configurable timeout and worker count
- Exit codes for integration with job schedulers

[0.2.0]: https://github.com/edsilegxrepo/go-pdf-extract/releases/tag/v0.2.0
[0.1.0]: https://github.com/edsilegxrepo/go-pdf-extract/releases/tag/v0.1.0
