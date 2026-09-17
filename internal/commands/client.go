package commands

import (
	"fmt"
	"sort"
	"strings"

	"ktd/internal/ai"
	"ktd/internal/store"
)

// The dials every fuzzy command reads, resolved by store.Setting — env var
// first, then a .env in the cwd, then one in the data dir — the same path
// the API key takes, so all of ktd's configuration can live in one file.
const (
	modelEnvName    = "KTD_MODEL"
	effortEnvName   = "KTD_EFFORT"
	thinkingEnvName = "KTD_THINKING"
)

// validEfforts and validThinking are checked up front so a typo'd dial
// fails with a list of what's accepted, rather than as an opaque 400 from
// the API halfway through an add.
var (
	validEfforts = map[string]bool{
		"low": true, "medium": true, "high": true, "xhigh": true, "max": true,
		ai.EffortOff: true,
	}
	validThinking = map[string]bool{
		ai.ThinkingOff: true, ai.ThinkingAdaptive: true,
	}
)

// newAIClient builds the AI client shared by every fuzzy command from the
// resolved API key and dials. Overriding the model is the reason the other
// two dials exist: effort and thinking aren't accepted by every model
// (Haiku 4.5 rejects effort outright), so pointing KTD_MODEL somewhere new
// may mean turning one of them off alongside it.
func newAIClient(s *store.Store) (*ai.Client, error) {
	apiKey, err := s.APIKey()
	if err != nil {
		return nil, err
	}

	effort := strings.ToLower(s.Setting(effortEnvName))
	if effort != "" && !validEfforts[effort] {
		return nil, fmt.Errorf("%s=%q is not one of %s", effortEnvName, effort, sortedKeys(validEfforts))
	}
	thinking := strings.ToLower(s.Setting(thinkingEnvName))
	if thinking != "" && !validThinking[thinking] {
		return nil, fmt.Errorf("%s=%q is not one of %s", thinkingEnvName, thinking, sortedKeys(validThinking))
	}

	return ai.NewClient(ai.Config{
		APIKey:   apiKey,
		Model:    s.Setting(modelEnvName),
		Effort:   effort,
		Thinking: thinking,
	}), nil
}

func sortedKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
