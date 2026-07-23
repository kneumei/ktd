package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// apiKeyEnvName is the only env var / .env key name ktd looks at for the
// Anthropic API key. Deliberately app-specific rather than the generic
// ANTHROPIC_API_KEY, so it never picks up a key set for another tool
// (Claude Code, etc.) by coincidence.
const apiKeyEnvName = "KTD_ANTHROPIC_API_KEY"

// APIKey resolves the Anthropic API key in order: (1) the
// KTD_ANTHROPIC_API_KEY env var; (2) a .env file in the current working
// directory (dev convenience — running from the source project); (3) a
// .env file in the data dir (the recommended permanent home once ktd is
// installed and run from arbitrary directories, since it always resolves
// to the same place).
func (s *Store) APIKey() (string, error) {
	if key := os.Getenv(apiKeyEnvName); key != "" {
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
		"no Anthropic API key found: set %s, or add %s=... to a .env file in the current directory or in %s",
		apiKeyEnvName, apiKeyEnvName, s.Dir,
	)
}

// readEnvFile does a minimal KEY=value scan for apiKeyEnvName. It does not
// attempt full .env-format compatibility (export prefixes, multiline
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
		if key != apiKeyEnvName {
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
