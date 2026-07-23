package extractor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SanitizePath cleans and validates a filesystem path to prevent path traversal attacks.
func SanitizePath(path string) (string, error) {
	return sanitizePathExt(path, false)
}

// SanitizeExecutablePath cleans and validates a filesystem path for executables.
// It allows system directories like /bin or /usr, while rejecting other invalid paths.
func SanitizeExecutablePath(path string) (string, error) {
	return sanitizePathExt(path, true)
}

func sanitizePathExt(path string, allowSystemDirs bool) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}

	for _, r := range path {
		if r < 32 {
			return "", fmt.Errorf("path contains invalid characters")
		}
	}

	cleaned := filepath.Clean(path)

	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path must be absolute")
	}

	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
	}

	if err := validatePathSecurityExt(absPath, allowSystemDirs); err != nil {
		return "", err
	}

	return absPath, nil
}

func validatePathSecurityExt(absPath string, allowSystemDirs bool) error {
	return validatePathSecurityOS(absPath, runtime.GOOS, allowSystemDirs)
}

func validatePathSecurityOS(absPath string, goos string, allowSystemDirs bool) error {
	normalized := filepath.Clean(absPath)

	if goos == "windows" {
		normalizedWin := strings.ReplaceAll(normalized, "/", "\\")
		if strings.HasPrefix(normalizedWin, `\\`) {
			parts := strings.Split(normalizedWin, `\`)
			nonEmpty := 0
			for _, p := range parts {
				if p != "" {
					nonEmpty++
				}
			}
			if nonEmpty < 4 {
				return fmt.Errorf("UNC paths must have at least server, share, directory, and file")
			}
			if nonEmpty >= 2 {
				share := parts[3]
				if strings.HasSuffix(strings.ToUpper(share), "$") {
					return fmt.Errorf("administrative shares are not allowed")
				}
			}
		} else if len(normalizedWin) >= 3 && normalizedWin[1] == ':' {
			parts := strings.Split(normalizedWin[3:], `\`)
			nonEmpty := 0
			for _, p := range parts {
				if p != "" {
					nonEmpty++
				}
			}
			if nonEmpty < 2 {
				return fmt.Errorf("files in root directory are not allowed")
			}
		} else {
			return fmt.Errorf("invalid Windows path format")
		}
	} else {
		normalizedUnix := strings.ReplaceAll(normalized, "\\", "/")
		if strings.HasPrefix(normalizedUnix, "//") {
			normalizedUnix = normalizedUnix[1:]
		}
		if normalizedUnix == "/" {
			return fmt.Errorf("root paths are not allowed")
		}

		parts := strings.Split(normalizedUnix, "/")
		nonEmpty := 0
		for _, p := range parts {
			if p != "" {
				nonEmpty++
			}
		}
		if nonEmpty < 2 {
			return fmt.Errorf("files in root directory are not allowed")
		}

		if !allowSystemDirs {
			systemDirs := []string{"/etc", "/usr", "/bin", "/sbin", "/boot", "/sys", "/proc"}
			for _, sysDir := range systemDirs {
				if normalizedUnix == sysDir || strings.HasPrefix(normalizedUnix, sysDir+"/") {
					return fmt.Errorf("system directory %s is not allowed", sysDir)
				}
			}
		}
	}

	return nil
}

// ValidateDirectory checks that a path exists and is a directory.
func ValidateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	return nil
}

// ValidateExecutable checks that a path points to a valid executable file.
// SECURITY: path must be pre-sanitized via SanitizeExecutablePath() before calling.
func ValidateExecutable(path string) error {
	// #nosec G703 -- path is pre-sanitized by SanitizeExecutablePath() in all callers
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not an executable")
	}

	if runtime.GOOS != "windows" {
		if info.Mode()&0o111 == 0 {
			return fmt.Errorf("file is not executable")
		}
	} else {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".exe" && ext != ".com" && ext != ".bat" && ext != ".cmd" {
			return fmt.Errorf("file does not have executable extension")
		}
	}
	return nil
}
