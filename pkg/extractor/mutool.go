package extractor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// FindMutool locates and validates the mutool binary.
// Search order (first valid wins):
//  1. Explicit path argument (from CLI flag)
//  2. MUTOOL_BIN environment variable
//  3. "mutool" in system PATH
func FindMutool(flagPath string) (string, error) {
	if flagPath != "" {
		return validateMutoolPath(flagPath, "-mutool-bin")
	}

	if envPath := os.Getenv("MUTOOL_BIN"); envPath != "" {
		return validateMutoolPath(envPath, "MUTOOL_BIN")
	}

	path, err := exec.LookPath("mutool")
	if err != nil {
		return "", fmt.Errorf("mutool not found in PATH, set MUTOOL_BIN or use -mutool-bin flag")
	}
	return validateMutoolPath(path, "PATH")
}

func validateMutoolPath(path, source string) (string, error) {
	cleanPath, err := SanitizeExecutablePath(path)
	if err != nil {
		return "", fmt.Errorf("invalid %s path: %v", source, err)
	}
	if err := ValidateExecutable(cleanPath); err != nil {
		return "", fmt.Errorf("mutool binary not valid at %s path %s: %v", source, cleanPath, err)
	}
	return cleanPath, nil
}

// TestMutoolExecution verifies that mutool can be executed successfully.
func TestMutoolExecution(mutoolPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// #nosec G204 -- mutoolPath is sanitized and validated by FindMutool()
	cmd := exec.CommandContext(ctx, mutoolPath, "-v")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("execution failed: %v (output: %s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}
