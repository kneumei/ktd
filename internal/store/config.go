package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// APIKey resolves the Anthropic API key in order: (1) the ANTHROPIC_API_KEY
// env var; (2) a .env file in the current working directory (dev
// convenience — running from the source project); (3) a .env file in the
// data dir (the recommended permanent home once ktd is installed and run
// from arbitrary directories, since it always resolves to the same place).
func (s *Store) APIKey() (string, error) {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return key, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if key, ok := readEnvFile(filepath.Join(cwd, ".env")); ok {
			return key, nil
		}
	}
	if key, ok := readEnvFile(filepath.Join(s.Dir, ".env")); ok {
		return key, nil
	}
	return "", fmt.Errorf(
		"no Anthropic API key found: set ANTHROPIC_API_KEY, or add ANTHROPIC_API_KEY=... to a .env file "+
			"in the current directory or in %s", s.Dir,
	)
}

// readEnvFile does a minimal KEY=value scan for ANTHROPIC_API_KEY. It does
// not attempt full .env-format compatibility (export prefixes, multiline
// values, etc.) — just enough for a single secret.
func readEnvFile(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if key != "ANTHROPIC_API_KEY" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if value != "" {
			return value, true
		}
	}
	return "", false
}
