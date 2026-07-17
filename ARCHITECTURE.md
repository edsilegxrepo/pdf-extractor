# go-pdf-extractor Architecture Document

## 1. Architecture and Design Choices

### 1.1 High-Level Architecture

The application follows a pipeline architecture with concurrent processing capabilities:

```mermaid
flowchart TB
    subgraph Input["Input Layer"]
        CLI[CLI Parser]
        ENV[Environment Variables]
        FS[Filesystem]
    end

    subgraph Validation["Validation Layer"]
        CFG[Config Validator]
        PATH[Path Sanitizer]
        EXEC[Executable Validator]
    end

    subgraph Processing["Processing Layer"]
        POOL[Worker Pool]
        subgraph Workers["Worker Goroutines"]
            W1[Worker 1]
            W2[Worker 2]
            WN[Worker N]
        end
        MUTOOL[mutool Subprocess]
        EXTRACT[Value Extractor]
    end

    subgraph Output["Output Layer"]
        JSON[JSON Writer]
        TSV[TSV Writer]
        FILE[Output File]
    end

    CLI --> CFG
    ENV --> CFG
    FS --> CFG
    CFG --> PATH
    PATH --> EXEC
    EXEC --> POOL
    POOL --> W1 & W2 & WN
    W1 & W2 & WN --> MUTOOL
    MUTOOL --> EXTRACT
    EXTRACT --> JSON & TSV
    JSON & TSV --> FILE
```

### 1.2 Design Decisions

#### 1.2.1 Worker Pool Pattern

**Decision**: Implement bounded worker pool using goroutines and channels.

**Rationale**:
- Predictable resource consumption with configurable concurrency limits
- Efficient utilization of multi-core systems for I/O-bound PDF processing
- Natural backpressure through buffered channels
- Clean shutdown semantics via channel closure

**Trade-offs**:
- Slight overhead from channel operations versus unbounded goroutines
- Fixed upper bound (16 workers) may underutilize high-core-count systems

#### 1.2.2 External Process for PDF Parsing

**Decision**: Use mutool as external subprocess rather than embedding a PDF library.

**Rationale**:
- MuPDF is a mature, well-tested PDF rendering library
- Avoids CGO complexity and cross-compilation issues
- Subprocess isolation provides fault tolerance (crashes do not affect main process)
- Simpler deployment (single binary plus mutool)

**Trade-offs**:
- Process spawn overhead per file
- Dependency on external binary availability
- Inter-process communication overhead

#### 1.2.3 NDJSON Output Format

**Decision**: Use newline-delimited JSON rather than JSON array.

**Rationale**:
- Streaming-friendly format for large result sets
- Each line is independently parseable
- Compatible with common log processing tools (jq, grep)
- Simpler error handling (partial output remains valid)

**Trade-offs**:
- Not valid JSON as a single document
- Requires line-by-line parsing by consumers

#### 1.2.4 Build Tags for Platform-Specific Code

**Decision**: Use Go build tags to separate Windows and Unix implementations.

**Rationale**:
- Process group handling differs fundamentally between platforms
- Clean separation without runtime conditionals
- Compiler excludes irrelevant code from each platform build

**Implementation**:
- `process_windows.go`: Uses `CREATE_NEW_PROCESS_GROUP` flag
- `process_unix.go`: Uses `Setpgid` and negative PID signals

### 1.3 Assumptions

| Assumption | Impact if Invalid |
|------------|-------------------|
| PDFs contain extractable text | No values extracted; null results returned |
| Search pattern appears on single line | Multi-line patterns not matched |
| mutool available and functional | Exit code 2 returned |
| Workspace directory is writable | N/A (only reads from workspace) |
| Output directory is writable | Exit code 5 returned |
| Filenames do not contain newlines | JSON output may be malformed |
| UTF-8 encoding in PDF text | Extraction may fail for other encodings |

### 1.4 Edge Cases

#### 1.4.1 Empty Workspace

**Scenario**: No files match the glob pattern.
**Behavior**: Empty output file created (empty JSON or TSV with header only).
**Exit Code**: 0 (Success)

#### 1.4.2 All Files Fail

**Scenario**: Every PDF fails processing (corrupt, password-protected, timeout).
**Behavior**: Output contains error entries for all files.
**Exit Code**: 10 (PartialFailure)

#### 1.4.3 Multiple Matches in Single File

**Scenario**: PDF contains search pattern multiple times.
**Behavior**: All unique values returned as array in JSON, comma-separated in TSV.
**Deduplication**: Identical values within same file are deduplicated.

#### 1.4.4 Very Large Files

**Scenario**: PDF processing exceeds timeout threshold.
**Behavior**: Process group killed, error recorded, other files continue.
**Mitigation**: Increase `-timeout` parameter for known large files.

#### 1.4.5 High File Count

**Scenario**: Thousands of files in workspace.
**Behavior**: Worker pool processes files concurrently within bounds.
**Memory**: Channels sized to file count; results accumulated in memory.
**Limitation**: Extremely large batches may exhaust memory.

### 1.5 Performance and Efficiency

#### 1.5.1 Concurrency Model Performance

| Factor | Impact |
|--------|--------|
| Worker count | Linear scaling up to I/O saturation |
| Channel buffering | Prevents goroutine blocking |
| Process spawn | ~10-50ms overhead per file |
| mutool execution | Dominant factor; varies with PDF size/complexity |

#### 1.5.2 Memory Characteristics

| Component | Memory Usage |
|-----------|--------------|
| Worker goroutines | ~8KB stack per worker |
| Job channel | Pointer size * file count |
| Result channel | Result struct size * file count |
| Result accumulation | All results held in memory |
| mutool output | Buffered per-file (released after extraction) |

#### 1.5.3 Recommended Worker Settings

| System Type | Recommended Workers |
|-------------|---------------------|
| 2-core system | 2-4 |
| 4-core system | 4-8 |
| 8+ core system | 8-16 |
| I/O-constrained (NFS, slow disk) | 2-4 |
| CPU-constrained | NumCPU |

## 2. Data Flow and Control Logic

### 2.1 Operational Flow

```mermaid
flowchart TD
    START([Start]) --> PARSE[Parse CLI Flags]
    PARSE --> VALIDATE{Validate Config}
    VALIDATE -->|Invalid| ERR_CFG[Exit 1: ConfigError]
    VALIDATE -->|Path Error| ERR_PATH[Exit 3: PathError]
    VALIDATE -->|Valid| MUTOOL[Find mutool Binary]
    MUTOOL -->|Not Found| ERR_MUT[Exit 2: MutoolNotFound]
    MUTOOL -->|Found| GLOB[Find Matching Files]
    GLOB -->|Pattern Error| ERR_PAT[Exit 4: PatternError]
    GLOB -->|Success| PROCESS[Process Files]
    PROCESS --> WRITE{Write Output}
    WRITE -->|Error| ERR_OUT[Exit 5: OutputError]
    WRITE -->|Success| CHECK{Any Errors?}
    CHECK -->|Yes| PARTIAL[Exit 10: PartialFailure]
    CHECK -->|No| SUCCESS[Exit 0: Success]
```

### 2.2 Code Relations

```mermaid
classDiagram
    class main {
        +main()
    }
    class Config {
        +Path string
        +FilePattern string
        +Search string
        +Format string
        +Output string
        +MutoolBin string
        +Timeout Duration
        +Workers int
    }
    class Result {
        +Filename string
        +Value interface
        +Error string
    }
    
    main --> parseFlags : creates
    main --> run : invokes
    parseFlags --> Config : returns
    run --> validateConfig : validates
    run --> findMutool : locates binary
    run --> findFiles : discovers PDFs
    run --> processFiles : concurrent processing
    run --> writeOutput : writes results
    
    validateConfig --> sanitizePath : path validation
    findMutool --> validateMutoolPath : binary validation
    validateMutoolPath --> sanitizePath : path cleaning
    validateMutoolPath --> validateExecutable : executable check
    
    processFiles --> processFile : per-file processing
    processFile --> setupProcessGroup : OS-specific
    processFile --> killProcessGroup : OS-specific
    processFile --> extractValues : pattern matching
    processFile --> Result : produces
    
    writeOutput --> writeJSON : JSON format
    writeOutput --> writeTSV : TSV format

    class sanitizePath {
        +sanitizePath(path) string, error
    }
    class validateExecutable {
        +validateExecutable(path) error
    }
    class validateMutoolPath {
        +validateMutoolPath(path, source) string, error
    }
```

### 2.3 Data Sequence

```mermaid
sequenceDiagram
    participant CLI as CLI/Main
    participant Val as Validator
    participant Pool as Worker Pool
    participant Worker as Worker Goroutine
    participant Mutool as mutool Process
    participant Extract as Extractor
    participant Writer as Output Writer

    CLI->>Val: parseFlags()
    Val-->>CLI: Config
    CLI->>Val: validateConfig(cfg)
    Val->>Val: sanitizePath(path)
    Val->>Val: sanitizePath(output)
    Val-->>CLI: nil or error

    CLI->>Val: findMutool(flagPath)
    Val->>Val: validateMutoolPath()
    Val-->>CLI: mutoolPath or error

    CLI->>Val: findFiles(path, pattern)
    Val-->>CLI: []string{files}

    CLI->>Pool: processFiles(files, mutoolPath, ...)
    
    loop For each file
        Pool->>Worker: file (via jobs channel)
        Worker->>Mutool: exec.CommandContext(mutool, args...)
        Worker->>Mutool: setupProcessGroup(cmd)
        Mutool-->>Worker: stdout (PDF text)
        
        alt Timeout or Error
            Worker->>Mutool: killProcessGroup(cmd)
            Worker-->>Pool: Result{Error: "..."}
        else Success
            Worker->>Extract: extractValues(text, search)
            Extract-->>Worker: []string{values}
            Worker-->>Pool: Result{Value: values}
        end
    end

    Pool-->>CLI: []Result

    CLI->>Writer: writeOutput(results, format, path)
    
    alt format == "json"
        Writer->>Writer: writeJSON()
    else format == "tsv"
        Writer->>Writer: writeTSV()
    end
    
    Writer-->>CLI: nil or error
    CLI-->>CLI: Determine exit code
```

### 2.4 Worker Pool Lifecycle

```mermaid
sequenceDiagram
    participant Main as Main Goroutine
    participant Jobs as Jobs Channel
    participant W1 as Worker 1
    participant W2 as Worker 2
    participant Results as Results Channel
    participant Collector as Result Collector

    Main->>Jobs: Create buffered channel(len(files))
    Main->>Results: Create buffered channel(len(files))
    
    Main->>W1: Start goroutine
    Main->>W2: Start goroutine
    Note over W1,W2: Workers block on Jobs channel

    loop For each file
        Main->>Jobs: Send file path
    end
    Main->>Jobs: Close channel

    par Worker Processing
        Jobs-->>W1: Receive file
        W1->>W1: processFile()
        W1->>Results: Send Result
    and
        Jobs-->>W2: Receive file
        W2->>W2: processFile()
        W2->>Results: Send Result
    end

    Note over W1,W2: Workers exit when Jobs closed and empty
    
    W1->>Main: WaitGroup.Done()
    W2->>Main: WaitGroup.Done()
    Main->>Results: Close channel (via goroutine after Wait)

    loop Until Results closed
        Results-->>Collector: Receive Result
        Collector->>Collector: Append to slice
    end
```

## 3. Dependencies

### 3.1 Go Standard Library Modules

| Package | Purpose |
|---------|---------|
| `bufio` | Buffered I/O for efficient file writing |
| `context` | Timeout and cancellation for subprocess management |
| `encoding/json` | JSON marshaling for output generation |
| `flag` | Command-line argument parsing |
| `fmt` | Formatted I/O and error messages |
| `os` | File operations, environment variables, process exit |
| `os/exec` | External process execution (mutool) |
| `path/filepath` | Cross-platform path manipulation and glob matching |
| `runtime` | CPU count detection for worker pool sizing |
| `strings` | String manipulation for value extraction |
| `sync` | WaitGroup for goroutine synchronization |
| `syscall` | Platform-specific process group operations |
| `time` | Duration parsing and timeout configuration |

### 3.2 External Utilities

| Utility | Version | Purpose | Installation |
|---------|---------|---------|--------------|
| mutool | 1.28.0+ | PDF text extraction | Part of MuPDF package |

### 3.3 Build Dependencies

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.21+ | Compilation |
| Git | Any | Source control |

### 3.4 Development Dependencies

| Tool | Purpose |
|------|---------|
| golangci-lint | Static analysis and linting |
| gosec | Security vulnerability scanning |
| gofumpt | Code formatting |
| govulncheck | Dependency vulnerability checking |

### 3.5 Test Dependencies

| Resource | Purpose |
|----------|---------|
| mutool | Integration tests require functional mutool |
| testfiles/*.pdf | Sample PDF files for functional testing |

### 3.6 Dependency Graph

```mermaid
flowchart LR
    subgraph Application["go-pdf-extractor"]
        MAIN[go-pdf-extractor.go]
        PROC_WIN[process_windows.go]
        PROC_UNIX[process_unix.go]
    end

    subgraph GoStdLib["Go Standard Library"]
        BUFIO[bufio]
        CONTEXT[context]
        JSON[encoding/json]
        FLAG[flag]
        FMT[fmt]
        OS[os]
        EXEC[os/exec]
        FILEPATH[path/filepath]
        RUNTIME[runtime]
        STRINGS[strings]
        SYNC[sync]
        SYSCALL[syscall]
        TIME[time]
    end

    subgraph External["External Dependencies"]
        MUTOOL[mutool binary]
        MUPDF[MuPDF library]
    end

    MAIN --> BUFIO & CONTEXT & JSON & FLAG & FMT
    MAIN --> OS & EXEC & FILEPATH & RUNTIME
    MAIN --> STRINGS & SYNC & TIME
    PROC_WIN --> EXEC & SYSCALL
    PROC_UNIX --> EXEC & SYSCALL
    EXEC --> MUTOOL
    MUTOOL --> MUPDF
```

### 3.7 Runtime Environment Requirements

| Requirement | Specification |
|-------------|---------------|
| Operating System | Windows Server 2016+, Linux (kernel 3.10+) |
| Architecture | amd64 (x86_64), arm64 |
| Memory | Minimum 128MB, recommended 512MB+ for large batches |
| Disk | Read access to PDF workspace, write access to output directory |
| Permissions | Execute permission for mutool binary |
