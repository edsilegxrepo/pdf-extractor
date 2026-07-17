# go-pdf-extract Design Document

## 1. Overview

### 1.1 Purpose

go-pdf-extract is a command-line utility that extracts delimiter-based values from PDF files using the MuPDF `mutool` binary. The tool processes batches of PDF files concurrently and outputs structured data in JSON or TSV format.

### 1.2 Business Requirements

The application addresses the following business needs:

1. **Document Routing**: Extract routing identifiers from signed DocuSign PDF documents to determine their destination in downstream systems.
2. **Batch Processing**: Process multiple PDF files in a single invocation to support high-volume document workflows.
3. **Integration Compatibility**: Provide machine-readable output formats suitable for consumption by workflow orchestration platforms.
4. **Operational Reliability**: Ensure predictable behavior with explicit exit codes for integration with job schedulers and monitoring systems.

### 1.3 Target Environment

- **Primary Integration**: GoAnywhere MFT (Managed File Transfer) platform
- **Downstream System**: Nuxeo ECM (Enterprise Content Management)
- **Document Source**: DocuSign signed PDF documents
- **Platforms**: Windows Server, Linux (RHEL/CentOS, Ubuntu)

## 2. Use Cases

### 2.1 Primary Use Case: DocuSign Document Routing

**Actors**: GoAnywhere MFT, go-pdf-extract, Nuxeo ECM

**Preconditions**:
- Signed PDF files have been deposited in a GoAnywhere workspace directory
- Each PDF contains a routing identifier in the format `DSFN:<value>`
- mutool binary is available on the system

**Flow**:
1. GoAnywhere receives one or more signed PDF files from DocuSign
2. GoAnywhere creates a uniquely-named workspace directory
3. GoAnywhere invokes go-pdf-extract with the workspace path and output parameters
4. go-pdf-extract extracts routing values from all PDFs and writes results to the output file
5. GoAnywhere parses the output file to determine routing rules for each document
6. Documents are routed to appropriate Nuxeo ECM destinations based on extracted values
7. Workspace directory is destroyed after processing completes

**Postconditions**:
- Output file contains one entry per PDF with filename and extracted value(s)
- Exit code indicates success (0), partial failure (10), or specific error condition (1-5)

### 2.2 Secondary Use Case: Batch Validation

**Purpose**: Validate that a set of PDFs contain expected routing identifiers before processing.

**Flow**:
1. Operator invokes go-pdf-extract with `-format tsv` for human-readable output
2. Output is reviewed to identify documents with missing or unexpected values
3. Documents with `null` values are flagged for manual review

### 2.3 Secondary Use Case: Alternative Pattern Extraction

**Purpose**: Extract values using different delimiter patterns for varied document types.

**Flow**:
1. Operator specifies custom `-search` pattern (e.g., `Invoice:`, `PO:`, `REF:`)
2. go-pdf-extract extracts values following the specified delimiter
3. Results are processed according to the custom pattern requirements

## 3. Integration Context

### 3.1 System Interfaces

```mermaid
flowchart LR
    subgraph Source["Document Source"]
        DOCUSIGN[DocuSign]
    end

    subgraph Orchestration["Workflow Orchestration"]
        GA[GoAnywhere MFT]
    end

    subgraph Processing["PDF Processing"]
        EXTRACT[go-pdf-extract]
        MUTOOL[mutool]
    end

    subgraph Destination["Content Management"]
        NUXEO[Nuxeo ECM]
    end

    DOCUSIGN -->|Signed PDFs| GA
    GA -->|Invoke CLI| EXTRACT
    EXTRACT -->|Text Extraction| MUTOOL
    MUTOOL -->|PDF Content| EXTRACT
    EXTRACT -->|Routing Data| GA
    GA -->|Route Documents| NUXEO
```

### 3.2 Input Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `-path` | string | Yes | - | Absolute or relative path to workspace directory containing PDF files |
| `-file-pattern` | string | Yes | - | Glob pattern for file selection (e.g., `*.pdf`, `invoice_*.pdf`) |
| `-search` | string | Yes | - | Delimiter pattern to search for in PDF text content |
| `-format` | string | Yes | - | Output format: `json` or `tsv` |
| `-output` | string | Yes | - | Path to output file (will be created or overwritten) |
| `-mutool-bin` | string | No | Auto-detect | Explicit path to mutool binary |
| `-workers` | int | No | NumCPU * 2 | Number of concurrent worker goroutines (min: 2, max: 16) |
| `-timeout` | duration | No | 30s | Timeout for each mutool invocation (e.g., `30s`, `1m`, `90s`) |
| `-version` | bool | No | false | Print version and exit |

### 3.3 Expected Inputs

**PDF Files**:
- Standard PDF format (PDF 1.0 through PDF 2.0)
- Text-based content (not scanned images requiring OCR)
- Accessible (not password-protected, unless mutool can handle)
- Contains search pattern on one or more lines

**Search Pattern Format**:
```
<delimiter><optional whitespace><value><end of line>
```

Example patterns in PDF content:
```
DSFN:Employee ID_X_X_X_X_Eag-AHP.pdf
DSFN: 327078_X_X_X_X_Wage.pdf
Invoice: INV-2024-001234
REF:PO-98765
```

### 3.4 Output Formats

#### 3.4.1 JSON Output (NDJSON)

Newline-delimited JSON with one object per line:

```json
{"filename":"doc1.pdf","value":"327078_X_X_X_X_Wage.pdf"}
{"filename":"doc2.pdf","value":["value1","value2"]}
{"filename":"doc3.pdf","value":null}
{"filename":"doc4.pdf","value":null,"error":"timeout exceeded"}
```

**Field Definitions**:
| Field | Type | Description |
|-------|------|-------------|
| `filename` | string | Base name of the processed PDF file |
| `value` | string, array, or null | Extracted value(s); null if no match found |
| `error` | string (optional) | Error message if processing failed |

#### 3.4.2 TSV Output

Tab-separated values with header row:

```
filename	value
doc1.pdf	327078_X_X_X_X_Wage.pdf
doc2.pdf	value1,value2
doc3.pdf	
doc4.pdf	
```

**Notes**:
- Multiple values are comma-separated in the value column
- Empty value column indicates no match or error
- Error details are not included in TSV format

## 4. Examples

### 4.1 Basic Usage

```bash
# Extract DSFN values from all PDFs in workspace
go-pdf-extract \
  -path /data/workspace/batch_20240115 \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /data/output/routing.json
```

### 4.2 Custom Worker Count

```bash
# Limit concurrency on resource-constrained systems
go-pdf-extract \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /tmp/results.json \
  -workers 4
```

### 4.3 Extended Timeout

```bash
# Process large PDFs with extended timeout
go-pdf-extract \
  -path /data/large_docs \
  -file-pattern "report_*.pdf" \
  -search "Reference:" \
  -format tsv \
  -output /tmp/references.tsv \
  -timeout 120s
```

### 4.4 Explicit mutool Path

```bash
# Use specific mutool installation
go-pdf-extract \
  -path /data/workspace \
  -file-pattern "*.pdf" \
  -search "DSFN:" \
  -format json \
  -output /tmp/results.json \
  -mutool-bin /opt/mupdf/bin/mutool
```

### 4.5 Windows Example

```powershell
# Windows PowerShell invocation
.\go-pdf-extract.exe `
  -path "D:\Data\Workspace\batch001" `
  -file-pattern "*.pdf" `
  -search "DSFN:" `
  -format json `
  -output "D:\Data\Output\routing.json"
```

### 4.6 Sample Output

**Input**: Two PDF files containing:
- `sample001.pdf`: Contains line `DSFN:Employee ID_X_X_X_X_Eag-AHP.pdf`
- `sample002.pdf`: Contains line `DSFN: 327078_X_X_X_X_Wage.pdf`

**JSON Output**:
```json
{"filename":"sample001.pdf","value":"Employee ID_X_X_X_X_Eag-AHP.pdf"}
{"filename":"sample002.pdf","value":"327078_X_X_X_X_Wage.pdf"}
```

**TSV Output**:
```
filename	value
sample001.pdf	Employee ID_X_X_X_X_Eag-AHP.pdf
sample002.pdf	327078_X_X_X_X_Wage.pdf
```

## 5. Concurrency Model

### 5.1 Worker Pool Architecture

The application uses a bounded worker pool pattern for concurrent PDF processing:

```mermaid
flowchart TB
    MAIN[Main Routine]
    
    subgraph Channels["Buffered Channels"]
        JOBS[(Jobs Channel<br/>buffered: N)]
        RESULTS[(Results Channel<br/>buffered: N)]
    end
    
    subgraph Pool["Worker Pool"]
        W1[Worker 1]
        W2[Worker 2]
        WN[Worker N]
    end
    
    MAIN --> JOBS
    MAIN --> RESULTS
    
    JOBS --> W1
    JOBS --> W2
    JOBS --> WN
    
    W1 --> RESULTS
    W2 --> RESULTS
    WN --> RESULTS
```

### 5.2 Worker Count Bounds

| Condition | Worker Count |
|-----------|--------------|
| Default (workers=0) | runtime.NumCPU() * 2 |
| Below minimum (workers=1) | 2 |
| Within bounds (2-16) | Specified value |
| Above maximum (workers>16) | 16 |

### 5.3 Thread Safety Guarantees

- **Channel-based communication**: All inter-goroutine data transfer uses Go channels
- **No shared mutable state**: Each worker operates on independent file paths
- **Synchronized completion**: sync.WaitGroup ensures all workers complete before result aggregation
- **Buffered channels**: Prevent goroutine blocking during high-throughput processing

## 6. Error Handling

### 6.1 Exit Codes

| Code | Constant | Condition | Output Written |
|------|----------|-----------|----------------|
| 0 | ExitSuccess | All files processed without errors | Yes |
| 1 | ExitConfigError | Invalid or missing CLI arguments | No |
| 2 | ExitMutoolNotFound | mutool binary not found or invalid | No |
| 3 | ExitPathError | Workspace path does not exist or is not a directory | No |
| 4 | ExitPatternError | Invalid glob pattern syntax | No |
| 5 | ExitOutputError | Cannot create or write to output file | No |
| 10 | ExitPartialFailure | Some PDFs failed processing | Yes |

### 6.2 Per-File Error Handling

When mutool fails on a specific file:
1. Error is captured in the Result struct
2. Processing continues for remaining files
3. Output file includes the error entry
4. Exit code 10 (PartialFailure) is returned

### 6.3 Timeout Handling

Each mutool invocation has an independent timeout:
1. Context with deadline is created per file
2. On timeout, process group is terminated
3. Result includes error message "timeout exceeded"
4. Other files continue processing

## 7. Constraints

### 7.1 Technical Constraints

| Constraint | Description |
|------------|-------------|
| **mutool Dependency** | Requires MuPDF mutool binary (version 1.28.0 or compatible) |
| **Text-based PDFs** | Cannot extract from scanned images; requires embedded text |
| **Line-based Matching** | Search pattern must appear on a single line |
| **No OCR** | Does not perform optical character recognition |

### 7.2 Operational Constraints

| Constraint | Description |
|------------|-------------|
| **File Naming** | GoAnywhere ensures unique filenames without special characters |
| **Workspace Lifecycle** | Workspace directories are ephemeral; destroyed after processing |
| **Single Invocation** | Designed for batch processing; not a daemon or service |

### 7.3 Security Constraints

| Constraint | Description |
|------------|-------------|
| **Input Sanitization** | All file paths and environment variables are sanitized |
| **No Network Access** | Application operates entirely on local filesystem |
| **Subprocess Isolation** | mutool runs in separate process group for clean termination |
| **No Credential Storage** | Application does not handle authentication or secrets |

### 7.4 Code Quality Constraints

| Constraint | Description |
|------------|-------------|
| **Portability** | Code must be fully portable across Windows and Linux using build tags for OS-specific code |
| **DRY Principle** | No code duplication; common logic extracted to shared functions |
| **Linting Compliance** | All linter exceptions must be justified with inline comments |
| **Test Coverage** | Minimum 80% code coverage; 100% functional coverage for CLI execution |

## 8. Testing

See [TESTING.md](TESTING.md) for the complete test plan including:

- Unit test specifications
- Integration test requirements
- Coverage requirements and metrics
- Test workspace management
