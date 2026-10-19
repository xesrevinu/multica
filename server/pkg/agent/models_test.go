package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStaticModelCatalogsAreValid(t *testing.T) {
	t.Parallel()
	catalogs := map[string][]Model{
		"claude": claudeStaticModels(),
		"codex":  codexStaticModels(),
	}
	for provider, models := range catalogs {
		if len(models) == 0 {
			t.Errorf("%s static catalog returned no models", provider)
		}
		defaultCount := 0
		for i, model := range models {
			if model.ID == "" {
				t.Errorf("%s static catalog[%d] has empty ID", provider, i)
			}
			if model.Label == "" {
				t.Errorf("%s static catalog[%d] has empty Label", provider, i)
			}
			if model.Default {
				defaultCount++
			}
		}
		if defaultCount > 1 {
			t.Errorf("%s: %d models marked Default, want 0 or 1", provider, defaultCount)
		}
	}
}
func TestFindACPConfigOptionRequiresExactID(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "config_options": [{
    "type": "select",
    "id": "thinking",
    "category": "thought_level",
    "current_value": "high"
  }]
}`)
	option, ok := findACPConfigOption(raw, "thinking")
	if !ok || option.ID != "thinking" {
		t.Fatalf("findACPConfigOption = (%+v, %v), want exact thinking option", option, ok)
	}
	value, ok := acpConfigOptionCurrentValue(raw, "thinking")
	if !ok || value != "high" {
		t.Errorf("acpConfigOptionCurrentValue = (%q, %v), want (high, true)", value, ok)
	}

	for _, invalidID := range []string{"reasoning", "Thinking", " thinking "} {
		renamed := []byte(fmt.Sprintf(`{"configOptions":[{"id":%q,"category":"thought_level","currentValue":"high"}]}`, invalidID))
		if option, ok := findACPConfigOption(renamed, "thinking"); ok {
			t.Fatalf("non-exact id %q exposed config option: %+v", invalidID, option)
		}
	}
}
func thinkingValues(thinking *ModelThinking) []string {
	if thinking == nil {
		return nil
	}
	values := make([]string, 0, len(thinking.SupportedLevels))
	for _, level := range thinking.SupportedLevels {
		values = append(values, level.Value)
	}
	return values
}

func TestModelKnownIncompatibleWithProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{
			name:     "claude model is incompatible with codex",
			provider: "codex",
			model:    "claude-sonnet-4-6",
			want:     true,
		},
		{
			name:     "codex model is compatible with codex",
			provider: "codex",
			model:    "gpt-5.5",
			want:     false,
		},
		{
			name:     "codex bundled gpt-6-astra is compatible",
			provider: "codex",
			model:    "gpt-6-astra",
			want:     false,
		},
		{
			name:     "codex model is incompatible with claude",
			provider: "claude",
			model:    "o3",
			want:     true,
		},
		{
			name:     "exact claude model is compatible with claude",
			provider: "claude",
			model:    "claude-opus-4-7",
			want:     false,
		},
		{
			name:     "claude context variant is compatible with claude",
			provider: "claude",
			model:    "claude-opus-5[1m]",
			want:     false,
		},
		{
			name:     "future-shaped claude context variant is compatible with claude",
			provider: "claude",
			model:    "claude-opus-5[500k]",
			want:     false,
		},
		{
			name:     "malformed claude context variant is incompatible with claude",
			provider: "claude",
			model:    "claude-opus-5[weird]",
			want:     true,
		},
		{
			name:     "future claude model stays compatible after context normalization",
			provider: "claude",
			model:    "claude-opus-5-5[1m]",
			want:     false,
		},
		{
			name:     "provider-prefixed openai model is incompatible with codex",
			provider: "codex",
			model:    "openai/gpt-4o",
			want:     true,
		},
		{
			name:     "provider-prefixed anthropic model is incompatible with claude",
			provider: "claude",
			model:    "anthropic/claude-opus-4.7",
			want:     true,
		},
		{
			name:     "future codex model is not rejected by the static fallback catalog",
			provider: "codex",
			model:    "gpt-6-sol",
			want:     false,
		},
		{
			name:     "unknown custom model is not classified",
			provider: "codex",
			model:    "private-lab-model",
			want:     false,
		},
		{
			name:     "unknown target provider does not clear",
			provider: "opencode",
			model:    "claude-sonnet-4-6",
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ModelKnownIncompatibleWithProvider(tc.provider, tc.model); got != tc.want {
				t.Fatalf("ModelKnownIncompatibleWithProvider(%q, %q) = %v, want %v", tc.provider, tc.model, got, tc.want)
			}
		})
	}
}
func TestListModelsUnknownProvider(t *testing.T) {
	ctx := context.Background()
	_, err := ListModels(ctx, "nonexistent", Command{Path: ""})
	if err == nil {
		t.Fatal("ListModels(unknown) expected error")
	}
}

func TestParseOpenCodeModels(t *testing.T) {
	input := `PROVIDER/MODEL                     CONTEXT  MAX_OUT
openai/gpt-4o                      128000   16384
anthropic/claude-sonnet-4-6        200000   8192
openai/gpt-4o                      128000   16384
nonprefixed-line
`
	models := parseOpenCodeModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (header skipped, duplicate deduped, non-slash skipped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" || models[0].Provider != "openai" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "anthropic/claude-sonnet-4-6" || models[1].Provider != "anthropic" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
}

func TestParseOpenCodeModelsVerboseVariants(t *testing.T) {
	input := `openai/gpt-5
{
  "id": "gpt-5",
  "name": "GPT-5",
  "reasoning": true,
  "variants": {
    "high": { "reasoningEffort": "high" },
    "low": { "reasoningEffort": "low" },
    "xhigh": { "reasoningEffort": "xhigh" },
    "fast-mode": { "reasoningEffort": "low" },
    "disabled": { "disabled": true }
  }
}
anthropic/claude-sonnet-4-6
{
  "id": "claude-sonnet-4-6",
  "reasoning": true,
  "variants": {
    "max": { "thinking": { "type": "enabled", "budgetTokens": 32000 } },
    "high": { "thinking": { "type": "enabled", "budgetTokens": 16000 } }
  }
}
`
	models := parseOpenCodeModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].Thinking == nil {
		t.Fatalf("expected first model to expose thinking variants")
	}
	got := make([]string, 0, len(models[0].Thinking.SupportedLevels))
	for _, lvl := range models[0].Thinking.SupportedLevels {
		got = append(got, lvl.Value)
		if lvl.Value == "xhigh" && lvl.Label != "Extra high" {
			t.Errorf("xhigh label: got %q, want Extra high", lvl.Label)
		}
		if lvl.Value == "fast-mode" && lvl.Label != "Fast Mode" {
			t.Errorf("custom variant label: got %q, want Fast Mode", lvl.Label)
		}
	}
	want := []string{"low", "high", "xhigh", "fast-mode"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("variant order/values: got %v, want %v", got, want)
	}
	if models[1].Thinking == nil || len(models[1].Thinking.SupportedLevels) != 2 {
		t.Fatalf("expected second model variants, got %+v", models[1].Thinking)
	}
}

func TestParseOpenCodeModelsMalformedVerboseBlockKeepsFollowingModels(t *testing.T) {
	input := `openai/gpt-5
{
  "id": "gpt-5",
  "reasoning": true,
  "variants": {
    "high": {}
  }
anthropic/claude-sonnet-4-6
{
  "id": "claude-sonnet-4-6",
  "reasoning": true,
  "variants": {
    "high": {},
    "max": {}
  }
}
`
	models := parseOpenCodeModels(input)
	if len(models) != 2 {
		t.Fatalf("expected both model rows to survive malformed JSON, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-5" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
	if models[0].Thinking != nil {
		t.Fatalf("malformed first JSON block should not annotate thinking: %+v", models[0].Thinking)
	}
	if models[1].ID != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("unexpected second model: %+v", models[1])
	}
	if models[1].Thinking == nil || len(models[1].Thinking.SupportedLevels) != 2 {
		t.Fatalf("valid following JSON block should still annotate thinking: %+v", models[1].Thinking)
	}
}

func TestDiscoverOpenCodeModelsFallsBackWhenVerboseFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fake := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
if [ "$1" = "models" ] && [ "$2" = "--verbose" ]; then
  exit 2
fi
if [ "$1" = "models" ]; then
  cat <<'EOF'
PROVIDER/MODEL                     CONTEXT  MAX_OUT
openai/gpt-4o                      128000   16384
EOF
  exit 0
fi
exit 1
`
	writeTestExecutable(t, fake, []byte(script))

	models, err := discoverOpenCodeModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatalf("discoverOpenCodeModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected fallback non-verbose model, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" || models[0].Thinking != nil {
		t.Fatalf("unexpected fallback model: %+v", models[0])
	}
}

// TestCachedDiscoveryDoesNotCacheEmpty verifies that an empty discovery result
// is not cached, so a transient failure (e.g. a `pi --list-models` timeout)
// doesn't keep the model picker blank for the full TTL. A non-empty result is
// still cached. See #3729.
func TestCachedDiscoveryDoesNotCacheEmpty(t *testing.T) {
	const emptyKey, nonEmptyKey = "test-cache-empty", "test-cache-nonempty"
	// modelCache is a package-level global; clear our keys up front and on
	// cleanup so the test stays hermetic under `go test -count=N` (a leftover
	// non-empty entry from a prior run would otherwise skip the callback).
	resetCache := func() {
		modelCacheMu.Lock()
		delete(modelCache, emptyKey)
		delete(modelCache, nonEmptyKey)
		modelCacheMu.Unlock()
	}
	resetCache()
	t.Cleanup(resetCache)

	emptyCalls := 0
	empty := func() (Catalog, error) {
		emptyCalls++
		return Catalog{Models: []Model{}}, nil
	}
	for i := 0; i < 2; i++ {
		got, err := cachedDiscovery(emptyKey, empty)
		if err != nil {
			t.Fatalf("cachedDiscovery: %v", err)
		}
		if len(got.Models) != 0 {
			t.Fatalf("expected empty result, got %+v", got)
		}
	}
	if emptyCalls != 2 {
		t.Fatalf("empty result must not be cached: expected fn called 2x, got %d", emptyCalls)
	}

	nonEmptyCalls := 0
	nonEmpty := func() (Catalog, error) {
		nonEmptyCalls++
		return Catalog{Models: []Model{{ID: "provider/model"}}}, nil
	}
	for i := 0; i < 2; i++ {
		if _, err := cachedDiscovery(nonEmptyKey, nonEmpty); err != nil {
			t.Fatalf("cachedDiscovery: %v", err)
		}
	}
	if nonEmptyCalls != 1 {
		t.Fatalf("non-empty result must be cached: expected fn called 1x, got %d", nonEmptyCalls)
	}
}

func writeFakePiRPCModelsBinary(t *testing.T) string {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
if [ "$1" = "--mode" ] && [ "$2" = "rpc" ]; then
  IFS= read -r _state_request
  IFS= read -r _models_request
  printf '%s\n' '{"id":"multica-state","type":"response","command":"get_state","success":true,"data":{"model":{"id":"gpt-5.6-luna","name":"Luna","provider":"openai-multi","reasoning":true,"thinkingLevelMap":{"off":"none","minimal":"none","low":"low","medium":null,"high":"high","xhigh":"xhigh","max":"max"}},"thinkingLevel":"max"}}'
  printf '%s\n' '{"id":"multica-models","type":"response","command":"get_available_models","success":true,"data":{"models":[{"id":"gpt-5.6-sol","name":"Sol","provider":"openai-multi","reasoning":true},{"id":"gpt-5.6-luna","name":"Luna","provider":"openai-multi","reasoning":true,"thinkingLevelMap":{"off":"none","minimal":"none","low":"low","medium":null,"high":"high","xhigh":"xhigh","max":"max"}},{"id":"plain-chat","name":"Plain chat","provider":"openai-multi","reasoning":false}]}}'
  exit 0
fi
printf '%s\n' 'provider model context max-out thinking images'
printf '%s\n' 'fallback fallback-model 128K 8K yes no'
`
	writeTestExecutable(t, fakePath, []byte(script))
	return fakePath
}

func TestDiscoverPiModelsRPCThinkingCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}
	fakePath := writeFakePiRPCModelsBinary(t)

	models, err := discoverPiModels(context.Background(), Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverPiModels: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 RPC models, got %d: %+v", len(models), models)
	}
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}

	sol := byID["openai-multi/gpt-5.6-sol"]
	if sol.Thinking == nil || !hasThinkingLevel(sol.Thinking, "medium") || hasThinkingLevel(sol.Thinking, "xhigh") || hasThinkingLevel(sol.Thinking, "max") {
		t.Fatalf("unexpected Sol thinking catalog: %+v", sol.Thinking)
	}
	luna := byID["openai-multi/gpt-5.6-luna"]
	if !luna.Default {
		t.Fatal("current Pi model must be marked as the runtime default")
	}
	if luna.Thinking == nil || luna.Thinking.DefaultLevel != "max" {
		t.Fatalf("unexpected Luna thinking default: %+v", luna.Thinking)
	}
	for _, level := range []string{"off", "minimal", "low", "high", "xhigh", "max"} {
		if !hasThinkingLevel(luna.Thinking, level) {
			t.Errorf("Luna missing level %q: %+v", level, luna.Thinking)
		}
	}
	if hasThinkingLevel(luna.Thinking, "medium") {
		t.Errorf("explicitly null Pi level must stay disabled: %+v", luna.Thinking)
	}
	if got := byID["openai-multi/plain-chat"].Thinking; got != nil {
		t.Fatalf("non-reasoning Pi model must not expose a picker: %+v", got)
	}
	if _, ok := byID["fallback/fallback-model"]; ok {
		t.Fatal("successful RPC discovery must not append the table fallback")
	}
}

func TestDiscoverPiModelsIDLessRPCErrorFallsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
if [ "$1" = "--mode" ] && [ "$2" = "rpc" ]; then
  IFS= read -r _state_request
  IFS= read -r _models_request
  printf '%s\n' '{"id":"multica-state","type":"response","command":"get_state","success":true,"data":{"thinkingLevel":"high"}}'
  printf '%s\n' '{"type":"response","command":"get_available_models","success":false,"error":"Unknown command: get_available_models"}'
  cat >/dev/null
  exit 0
fi
printf '%s\n' 'provider model context max-out thinking images'
printf '%s\n' 'fallback fallback-model 128K 8K yes no'
`
	writeTestExecutable(t, fakePath, []byte(script))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	models, err := discoverPiModels(ctx, Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverPiModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "fallback/fallback-model" {
		t.Fatalf("ID-less RPC error must terminate that request and use the table fallback, got %+v", models)
	}
}

func TestDiscoverPiModelsHungRPCPreservesTableFallbackBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
if [ "$1" = "--mode" ] && [ "$2" = "rpc" ]; then
  IFS= read -r _state_request
  IFS= read -r _models_request
  exec sleep 30
fi
printf '%s\n' 'provider model context max-out thinking images'
printf '%s\n' 'fallback fallback-model 128K 8K yes no'
`
	writeTestExecutable(t, fakePath, []byte(script))

	started := time.Now()
	models, err := discoverPiModelsWithin(context.Background(), Command{Path: fakePath}, 100*time.Millisecond, time.Second)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("discoverPiModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "fallback/fallback-model" {
		t.Fatalf("hung RPC must leave time for the table fallback, got %+v after %s", models, elapsed)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("RPC phase consumed the table fallback budget: elapsed %s", elapsed)
	}
}

func TestParsePiModels(t *testing.T) {
	input := `openai:gpt-4o
anthropic:claude-opus-4-7
openai:gpt-4o
bareword
`
	models := parsePiModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" {
		t.Errorf("expected colon normalized to slash: %+v", models[0])
	}
}

func TestParsePiModelsTableFormat(t *testing.T) {
	input := `provider             model                   context  max-out  thinking  images
bailian-coding-plan  glm-4.7                 202.8K   16.4K    no        no
bailian-coding-plan  qwen3.6-plus            1M       65.5K    no        yes
opencode             claude-sonnet-4-6       1M       64K      yes       yes
opencode             claude-sonnet-4-6:exp   1M       64K      yes       yes
opencode             claude-sonnet-4-6       1M       64K      yes       yes
bareword-only-line
`
	models := parsePiModels(input)
	if len(models) != 4 {
		t.Fatalf("expected 4 models (header skipped, duplicate deduped, bareword skipped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "bailian-coding-plan/glm-4.7" || models[0].Provider != "bailian-coding-plan" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "bailian-coding-plan/qwen3.6-plus" || models[1].Provider != "bailian-coding-plan" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
	if models[2].ID != "opencode/claude-sonnet-4-6" || models[2].Provider != "opencode" {
		t.Errorf("unexpected third model: %+v", models[2])
	}
	// Colon inside a model name in column 1 must be preserved — only
	// the legacy `provider:model` form gets colon→slash normalization.
	if models[3].ID != "opencode/claude-sonnet-4-6:exp" || models[3].Provider != "opencode" {
		t.Errorf("expected ':' inside table-format model name to be preserved: %+v", models[3])
	}
}

// TestParsePiModelsSkipsForkUsageHints pins the second half of GitHub #4482: a
// pi-family custom runtime profile can point at a fork with no `--list-models`,
// which exits printing usage text. Those lines carry no diagnostic prefix, so
// the field splitter used to coin them into models like `Run/`omp`. An empty
// catalog is the correct answer — the UI falls back to manual entry, which is
// strictly better than offering IDs the CLI will reject.
func TestParsePiModelsSkipsForkUsageHints(t *testing.T) {
	input := "Error: unknown flag: --list-models\n" +
		"Run `omp --help` for available flags.\n" +
		"Usage: omp [command]\n" +
		"unknown command \"models\" for \"omp\"\n"

	if models := parsePiModels(input); len(models) != 0 {
		t.Fatalf("expected usage text to yield no models, got %+v", models)
	}
}

// TestParsePiModelsKeepsCatalogAlongsideUsageHints pins that the widened noise
// filter only drops the prose: a real catalog printed next to a usage hint
// still parses. Without this the #3729 behaviour (catalog on a non-zero exit)
// could be silently traded away for the #4482 fix.
func TestParsePiModelsKeepsCatalogAlongsideUsageHints(t *testing.T) {
	input := "Run `omp --help` for available flags.\n" +
		"provider  model    context\n" +
		"opencode  glm-4.7  202.8K\n"

	models := parsePiModels(input)
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	if models[0].ID != "opencode/glm-4.7" {
		t.Errorf("unexpected model: %+v", models[0])
	}
}

// TestDiscoverPiModelsNonZeroExit verifies that discoverPiModels still returns
// the resolvable catalog when `pi --list-models` exits non-zero. Pi exits
// non-zero (and warns) when an agent config references stale provider/model
// patterns that no longer match the local catalog. Before the fix the daemon
// discarded the populated output on any non-zero exit and returned an empty
// list, so the UI model picker was blank even though the runtime was online and
// agents ran fine. See GitHub #3729.
func TestDiscoverPiModelsNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}

	const table = "provider         model        context  max-out  thinking  images\n" +
		"glm-coding-plan  glm-4.7      202.8K   16.4K    no        no"
	// The unmatched-pattern warning, with and without the `Warning:` prefix —
	// the prefix is not guaranteed across pi versions, and the bare form is
	// what slips past a naive guard into a bogus `No/models` model.
	const prefixed = `Warning: No models match pattern "opencode-go/mimo-v2-omni"`
	const bare = `No models match pattern "opencode-go/mimo-v2-pro"`

	cases := []struct {
		name   string
		script string
	}{
		{
			// Newer pi prints the catalog to stdout; the stale-pattern
			// warning goes to stderr and the process exits non-zero.
			name: "catalog on stdout",
			script: "#!/bin/sh\n" +
				"cat <<'EOF'\n" + table + "\nEOF\n" +
				"echo " + strconv.Quote(prefixed) + " >&2\n" +
				"exit 1\n",
		},
		{
			// Older pi prints the catalog (and the warning) to stderr; same
			// non-zero exit. The stderr fallback must still parse the catalog.
			name: "catalog and prefixed warning on stderr",
			script: "#!/bin/sh\n" +
				"cat >&2 <<'EOF'\n" + table + "\n" + prefixed + "\nEOF\n" +
				"exit 1\n",
		},
		{
			// Same, but the warning has no `Warning:` prefix — must not leak in
			// as a `No/models` row.
			name: "catalog and bare warning on stderr",
			script: "#!/bin/sh\n" +
				"cat >&2 <<'EOF'\n" + table + "\n" + bare + "\nEOF\n" +
				"exit 1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakePath := filepath.Join(t.TempDir(), "pi")
			writeTestExecutable(t, fakePath, []byte(tc.script))

			models, err := discoverPiModels(context.Background(), Command{Path: fakePath})
			if err != nil {
				t.Fatalf("discoverPiModels: %v", err)
			}
			// Exactly the resolvable model — no warning line coined into a
			// bogus entry, no header row.
			if len(models) != 1 || models[0].ID != "glm-coding-plan/glm-4.7" {
				t.Fatalf("expected exactly [glm-coding-plan/glm-4.7] despite non-zero exit, got %+v", models)
			}
			if models[0].Thinking != nil {
				t.Fatalf("human-table fallback must not guess thinking levels: %+v", models[0].Thinking)
			}
		})
	}
}

// TestDiscoverOpenCodeModelsFallsBackOnVerboseNoise verifies that a non-zero
// `opencode models --verbose` whose stdout is unparseable noise still falls
// back to the plain `opencode models` command instead of returning empty. The
// earlier fix skipped the fallback whenever verbose printed any bytes, which
// regressed this case. Mirrors the pi hardening in #3729.
func TestDiscoverOpenCodeModelsFallsBackOnVerboseNoise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode binary is a /bin/sh script")
	}

	// `opencode models --verbose` => $2 == "--verbose": emit noise + exit 1.
	// `opencode models`           => no $2: print the plain catalog.
	script := "#!/bin/sh\n" +
		"if [ \"$2\" = \"--verbose\" ]; then\n" +
		"  echo 'panic: catalog sync failed'\n" +
		"  exit 1\n" +
		"fi\n" +
		"echo 'openai/gpt-4o'\n"

	fakePath := filepath.Join(t.TempDir(), "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	models, err := discoverOpenCodeModels(context.Background(), Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverOpenCodeModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "openai/gpt-4o" {
		t.Fatalf("expected fallback to plain `opencode models` to yield [openai/gpt-4o], got %+v", models)
	}
}
func TestParseACPSessionNewModelsFromConfigOptions(t *testing.T) {
	// Trimmed copy of a real kimi 0.29 session/new result.
	raw := []byte(`{
      "sessionId": "session_abc",
      "configOptions": [
        {
          "type": "select",
          "id": "model",
          "name": "Model",
          "category": "model",
          "currentValue": "kimi-code/k3",
          "options": [
            {"value": "kimi-code/kimi-for-coding", "name": "K2.7 Coding"},
            {"value": "kimi-code/kimi-for-coding-highspeed", "name": "K2.7 Coding Highspeed"},
            {"value": "kimi-code/k3", "name": "K3"}
          ]
        },
        {
          "type": "select",
          "id": "thinking",
          "category": "thought_level",
          "currentValue": "high",
          "options": [
            {"value": "low", "name": "Low"},
            {"value": "high", "name": "High"},
            {"value": "max", "name": "Max"}
          ]
        }
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 3 {
		t.Fatalf("expected 3 models from configOptions, got %d: %+v", len(models), models)
	}
	if models[0].ID != "kimi-code/kimi-for-coding" || models[0].Label != "K2.7 Coding" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	// `kimi-code/k3` has no colon, so it must not be split into a provider
	// group off the slash.
	if models[2].Provider != "" {
		t.Errorf("slash-form model id must not derive a provider: %+v", models[2])
	}
	if !models[2].Default {
		t.Errorf("currentValue entry must be marked default: %+v", models[2])
	}
	for _, m := range models {
		if m.ID == "low" || m.ID == "high" || m.ID == "max" {
			t.Errorf("thinking-level option leaked into the model catalog: %+v", m)
		}
	}
}

// The `models` block stays authoritative: an agent emitting both shapes
// must not have its catalog replaced by configOptions.
func TestParseACPSessionNewModelsPrefersModelsBlockOverConfigOptions(t *testing.T) {
	raw := []byte(`{
      "sessionId": "session_abc",
      "models": {
        "availableModels": [{"modelId": "nous:anthropic/claude-opus-4.7", "name": "Opus"}],
        "currentModelId": "nous:anthropic/claude-opus-4.7"
      },
      "configOptions": [
        {"id": "model", "category": "model", "currentValue": "other/one",
         "options": [{"value": "other/one", "name": "Other"}]}
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 1 || models[0].ID != "nous:anthropic/claude-opus-4.7" {
		t.Fatalf("models block must win over configOptions, got %+v", models)
	}
}

func TestParseACPSessionNewModelsConfigOptionsSnakeCaseAndCategoryOnly(t *testing.T) {
	// No `id: "model"` — the option is identified by category alone — and
	// the response uses snake_case keys.
	raw := []byte(`{
      "session_id": "session_abc",
      "config_options": [
        {
          "id": "primary_model",
          "category": "MODEL",
          "current_value": "kimi-code/k3",
          "options": [
            {"value": "kimi-code/k3", "name": "K3"},
            {"value": "kimi-code/k3", "name": "duplicate"},
            {"value": "  ", "name": "blank"},
            {"value": "kimi-code/kimi-for-coding", "name": ""}
          ]
        }
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (duplicate and blank dropped), got %d: %+v", len(models), models)
	}
	if !models[0].Default {
		t.Errorf("snake_case current_value should mark default: %+v", models[0])
	}
	if models[1].Label != "kimi-code/kimi-for-coding" {
		t.Errorf("missing name should fall back to the model id, got %+v", models[1])
	}
}

func TestParseACPSessionNewModelsIgnoresNonModelConfigOptions(t *testing.T) {
	// session/new with configOptions but no model picker must still yield an
	// empty catalog rather than inventing one from the thinking levels.
	raw := []byte(`{
      "sessionId": "session_abc",
      "configOptions": [
        {"id": "thinking", "category": "thought_level", "currentValue": "high",
         "options": [{"value": "low", "name": "Low"}, {"value": "high", "name": "High"}]}
      ]
    }`)
	if got := parseACPSessionNewModels(raw); len(got) != 0 {
		t.Errorf("expected empty catalog, got %+v", got)
	}
}

func TestACPResultTopLevelKeys(t *testing.T) {
	// Diagnostic line must expose key names only — never the values that
	// carry session ids or catalog contents.
	keys := acpResultTopLevelKeys([]byte(`{"sessionId":"session_secret","configOptions":[],"modes":{}}`))
	got := strings.Join(keys, ",")
	if got != "configOptions,modes,sessionId" {
		t.Errorf("unexpected keys: %q", got)
	}
	if acpResultTopLevelKeys([]byte("not json")) != nil {
		t.Error("expected nil for non-JSON result")
	}
}
func TestCachedDiscovery(t *testing.T) {
	calls := 0
	fn := func() (Catalog, error) {
		calls++
		return Catalog{Models: []Model{{ID: "x", Label: "x"}}}, nil
	}
	// First call populates the cache; reset for isolation.
	modelCacheMu.Lock()
	delete(modelCache, "testkey")
	modelCacheMu.Unlock()

	if _, err := cachedDiscovery("testkey", fn); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedDiscovery("testkey", fn); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected 1 underlying call due to cache, got %d", calls)
	}
}

// TestDiscoveryCacheKeyIsolatesByExecutable verifies that two different
// executable paths for the same provider type produce different cache keys,
// so a built-in and a custom Dim-compatible executable do not serve each
// other's model catalog during the TTL.
func TestDiscoveryCacheKeyIsolatesByExecutable(t *testing.T) {
	key1 := discoveryCacheKey("dim", Command{Path: "/usr/bin/dim"})
	key2 := discoveryCacheKey("dim", Command{Path: "/opt/custom/dim"})
	if key1 == key2 {
		t.Fatalf("different executables must produce different cache keys: both %q", key1)
	}
	keyEmpty := discoveryCacheKey("dim", Command{Path: ""})
	if keyEmpty != "dim" {
		t.Fatalf("empty executable should fall back to provider type, got %q", keyEmpty)
	}
	if keyEmpty == key1 {
		t.Fatal("empty executable key must differ from a non-empty executable key")
	}

	calls := 0
	fn := func() (Catalog, error) {
		calls++
		return Catalog{Models: []Model{{ID: "m"}}}, nil
	}
	modelCacheMu.Lock()
	delete(modelCache, key1)
	delete(modelCache, key2)
	modelCacheMu.Unlock()

	if _, err := cachedDiscovery(key1, fn); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedDiscovery(key2, fn); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("expected 2 underlying calls (one per executable), got %d", calls)
	}
}

// TestQualifyModelID covers GH #7300: a persisted model id that
// omits its provider must be promoted to the catalog's canonical selector,
// but only when exactly one provider claims it. opencode rejects an
// unqualified id outright, and every capability lookup keyed on the model id
// misses until the two agree.
func TestQualifyModelID(t *testing.T) {
	t.Parallel()

	gateway := []Model{
		{ID: "multica-anthropic/claude/claude-opus-5", Provider: "multica-anthropic"},
		{ID: "multica-anthropic/claude/claude-sonnet-5", Provider: "multica-anthropic"},
		{ID: "multica-codex/codex/gpt-5.6-sol", Provider: "multica-codex"},
	}

	tests := []struct {
		name          string
		catalog       Catalog
		model         string
		want          string
		wantRewritten bool
	}{
		{
			name:          "slash-shaped id gains its provider",
			catalog:       Catalog{Models: gateway},
			model:         "claude/claude-opus-5",
			want:          "multica-anthropic/claude/claude-opus-5",
			wantRewritten: true,
		},
		{
			name:    "already canonical is left alone",
			catalog: Catalog{Models: gateway},
			model:   "multica-anthropic/claude/claude-opus-5",
			want:    "multica-anthropic/claude/claude-opus-5",
		},
		{
			name: "an exact catalog id wins over a qualifiable one",
			// Both a bare `shared-id` model and a `vendor/shared-id` model
			// exist. The exact match is what the user picked; promoting it to
			// the other provider's entry would silently reroute the task.
			catalog: Catalog{Models: []Model{
				{ID: "shared-id", Provider: ""},
				{ID: "vendor/shared-id", Provider: "vendor"},
			}},
			model: "shared-id",
			want:  "shared-id",
		},
		{
			name: "ambiguous across providers stays untouched",
			catalog: Catalog{Models: []Model{
				{ID: "gateway-a/claude/claude-opus-5", Provider: "gateway-a"},
				{ID: "gateway-b/claude/claude-opus-5", Provider: "gateway-b"},
			}},
			model: "claude/claude-opus-5",
			want:  "claude/claude-opus-5",
		},
		{
			name:    "unknown model is passed through for the CLI to judge",
			catalog: Catalog{Models: gateway},
			model:   "something-nobody-advertises",
			want:    "something-nobody-advertises",
		},
		{
			name:    "empty catalog cannot qualify anything",
			catalog: Catalog{},
			model:   "claude/claude-opus-5",
			want:    "claude/claude-opus-5",
		},
		{
			// A static stand-in is not what the runtime actually supports, so
			// promoting against it would invent an id the CLI never advertised.
			name:    "a fallback catalog is never authoritative",
			catalog: Catalog{Models: gateway, Fallback: true},
			model:   "claude/claude-opus-5",
			want:    "claude/claude-opus-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, rewritten := QualifyModelID(tt.catalog, tt.model)
			if got != tt.want || rewritten != tt.wantRewritten {
				t.Errorf("QualifyModelID(%q) = (%q, %v), want (%q, %v)",
					tt.model, got, rewritten, tt.want, tt.wantRewritten)
			}
		})
	}
}

func TestAmbiguousModelSelectors(t *testing.T) {
	t.Parallel()

	shared := Catalog{Models: []Model{
		{ID: "OpenAI/grok-4.6", Provider: "OpenAI"},
		{ID: "SubGrok/grok-4.6", Provider: "SubGrok"},
		{ID: "xai/grok-4.6", Provider: "xai"},
	}}

	if got := AmbiguousModelSelectors(shared, "grok-4.6"); strings.Join(got, ",") != "OpenAI/grok-4.6,SubGrok/grok-4.6,xai/grok-4.6" {
		t.Errorf("AmbiguousModelSelectors(grok-4.6) = %v", got)
	}
	if got := AmbiguousModelSelectors(shared, "SubGrok/grok-4.6"); got != nil {
		t.Errorf("already-canonical id must not be ambiguous, got %v", got)
	}
	if got := AmbiguousModelSelectors(shared, "nobody"); got != nil {
		t.Errorf("unknown id must not be ambiguous, got %v", got)
	}
	if got := AmbiguousModelSelectors(Catalog{Models: shared.Models, Fallback: true}, "grok-4.6"); got != nil {
		t.Errorf("fallback catalog must not report claims, got %v", got)
	}
}

// A blank model means "let the runtime pick its own default" — there is
// nothing to qualify, and the runtime resolves its own selection.
func TestQualifyModelIDIgnoresBlankModel(t *testing.T) {
	t.Parallel()
	catalog := Catalog{Models: []Model{{ID: "vendor/only-model", Provider: "vendor"}}}
	got, rewritten := QualifyModelID(catalog, "   ")
	if got != "" || rewritten {
		t.Errorf("QualifyModelID(blank) = (%q, %v), want (\"\", false)", got, rewritten)
	}
}

// TestSlashShapedPiModelKeepsItsThinkingCatalog walks the chain that GH #7300
// reported as a dropped thinking_level: pi's RPC catalog carries a
// gateway-style model whose own id contains a slash, the agent persisted that
// bare id, and every capability lookup keyed on it missed. Qualifying the id
// first is what puts the persisted value back on the catalog entry that
// actually advertises the levels.
func TestSlashShapedPiModelKeepsItsThinkingCatalog(t *testing.T) {
	t.Parallel()

	// Verbatim shape of a real `get_available_models` RPC response for the
	// reporter's models.json.
	raw := []piRPCModel{
		{ID: "claude/claude-opus-5", Name: "Claude Opus 5", Provider: "multica-anthropic", Reasoning: true},
		{ID: "claude/claude-sonnet-5", Name: "Claude Sonnet 5", Provider: "multica-anthropic", Reasoning: true},
	}
	models := piModelsFromRPC(raw, piRPCState{})

	qualified, rewritten := QualifyModelID(Catalog{Models: models}, "claude/claude-opus-5")
	if !rewritten || qualified != "multica-anthropic/claude/claude-opus-5" {
		t.Fatalf("qualified = (%q, %v), want (%q, true)",
			qualified, rewritten, "multica-anthropic/claude/claude-opus-5")
	}

	var thinking *ModelThinking
	for _, m := range models {
		if m.ID == qualified {
			thinking = m.Thinking
		}
	}
	if thinking == nil {
		t.Fatalf("qualified model %q advertises no thinking catalog", qualified)
	}
	if !piThinkingSupports(thinking, "high") {
		t.Errorf("thinking catalog for %q missing \"high\": %+v", qualified, thinking.SupportedLevels)
	}
}

// TestModelSelectorMustBeProviderQualifiedIsAnExecutionContract pins what the
// predicate actually claims: whether a runtime's CLI refuses a model id that
// carries no provider prefix. It is deliberately NOT a statement about catalog
// shape — several runtimes (pi, omp, deveco) emit provider-prefixed ids, but
// only the ones whose CLI *rejects* the unprefixed form justify spending a
// discovery subprocess before launch.
//
// Pi 0.84+ (#7327) still accepts a uniquely authenticated bare id, so a bare
// Pi selector does not by itself justify a discovery subprocess. Ambiguous
// ids fail closed only when the daemon is already reading the catalog for a
// capability override. --model still receives the catalog ID whole;
// --provider is never synthesized (GH #7300).
func TestModelSelectorMustBeProviderQualifiedIsAnExecutionContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		want     bool
		why      string
	}{
		{"opencode", true, "run --model resolves strictly through provider/model"},
		{"pi", false, "bare ids still launch; ambiguity is checked on a shared catalog read"},
		{"omp", false, "pi-family fork, inherits pi's launch contract"},
		{"claude", false, "bare model ids, no provider segment to miss"},
		{"codex", false, "bare model ids"},
		{"dsh", false, "resolves its own provider-prefixed ids"},
		{"", false, "unknown provider must not spend discovery"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			if got := ModelSelectorMustBeProviderQualified(tt.provider); got != tt.want {
				t.Errorf("ModelSelectorMustBeProviderQualified(%q) = %v, want %v (%s)",
					tt.provider, got, tt.want, tt.why)
			}
		})
	}
}

// This fixture accepts OMP's discovery surface and rejects Pi-only flags (#8379).
func TestCustomOmpCompatibilityTargetDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	path := filepath.Join(t.TempDir(), "custom-wrapper")
	writeTestExecutable(t, path, []byte(`#!/bin/sh
[ "$1" = "launch" ] || exit 3
shift
if [ "$1 $2" = "models --json" ]; then
 echo '{"models":[{"provider":"commandcode","id":"deepseek/deepseek-v4.1-flash","selector":"commandcode/deepseek/deepseek-v4.1-flash"}]}'
 exit 0
fi
echo "Error: unknown flags: $*" >&2
exit 2
`))
	cmd := NewCommand(path, []string{"launch"})
	if _, err := ListModels(context.Background(), "pi", cmd); err == nil || !strings.Contains(err.Error(), "unknown flags") {
		t.Fatalf("Pi compatibility target must report failed probes, got %v", err)
	}
	catalog, err := ListModels(context.Background(), "omp", cmd)
	if err != nil || len(catalog.Models) != 1 || catalog.Models[0].ID != "commandcode/deepseek/deepseek-v4.1-flash" {
		t.Fatalf("OMP discovery lost selector or command prefix: %+v, %v", catalog, err)
	}
}

func TestOmpProfileLaunchPrefixUsesProtocolBlocklist(t *testing.T) {
	prefix := []string{"launch", "--mode", "text", "--model", "commandcode/deepseek/model"}
	got := FilterLaunchPrefix("omp", prefix, nil)
	want := FilterLaunchPrefix("pi", prefix, nil)
	if strings.Join(got, " ") != strings.Join(want, " ") || len(got) == len(prefix) || got[0] != "launch" {
		t.Fatalf("OMP prefix = %v; Pi prefix = %v", got, want)
	}
}
