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

// Setting resolves one configuration value by name, in order: (1) the env
// var of that name; (2) a .env file in the current working directory (dev
// convenience — running from the source project); (3) a .env file in the
// data dir (the recommended permanent home once ktd is installed and run
// from arbitrary directories, since it always resolves to the same place).
// Returns "" when the name is set nowhere. Every ktd dial resolves through
// here, so they can all be set the same way and live together in one .env
// rather than being exported by hand on every shell.
func (s *Store) Setting(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if cwd, err := os.Getwd(); err == nil {
		if v, ok := readEnvFile(filepath.Join(cwd, ".env"), name); ok {
			return v
		}
	}
	if v, ok := readEnvFile(filepath.Join(s.Dir, ".env"), name); ok {
		return v
	}
	return ""
}

// APIKey resolves the Anthropic API key like any other setting, but it's
// the one ktd can't run the fuzzy commands without, so a miss is an error
// carrying the instructions rather than an empty string.
func (s *Store) APIKey() (string, error) {
	if key := s.Setting(apiKeyEnvName); key != "" {
		return key, nil
	}
	return "", fmt.Errorf(
		"no Anthropic API key found: set %s, or add %s=... to a .env file in the current directory or in %s",
		apiKeyEnvName, apiKeyEnvName, s.Dir,
	)
}

// readEnvFile does a minimal KEY=value scan for one key. It does not
// attempt full .env-format compatibility (export prefixes, multiline
// values, etc.) — just enough for a secret and a handful of dials.
func readEnvFile(path, name string) (string, bool) {
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
		if key != name {
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
