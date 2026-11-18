package config

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestRegister_DefaultFileAtModuleRoot(t *testing.T) {
	root := projectDir(t, "TT_ROOT_KEY=found\n", true)
	sub := filepath.Join(root, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	svc := registerWith(t, &ConfigModuleOptions{})
	expectValues(t, svc, "E8", map[string]any{"TT_ROOT_KEY": "found"})
}

func TestRegister_DefaultFileUsesNearestGoMod(t *testing.T) {
	outer := projectDir(t, "TT_OUTER_KEY=outer\n", true)
	inner := filepath.Join(outer, "inner")
	pkg := filepath.Join(inner, "pkg")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "go.mod"), []byte("module example.test/inner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(pkg)

	svc := registerWith(t, &ConfigModuleOptions{})
	expectAbsent(t, svc, "E8: the nearest go.mod directory decides, a .env above it is not read", "TT_OUTER_KEY")
}

func TestRegister_DefaultFileInWorkingDirectoryWithoutGoMod(t *testing.T) {
	dir := t.TempDir()
	for parent := dir; ; parent = filepath.Dir(parent) {
		if _, err := os.Stat(filepath.Join(parent, "go.mod")); err == nil {
			t.Skip("a go.mod exists above the temporary directory")
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TT_CWD_KEY=cwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	svc := registerWith(t, &ConfigModuleOptions{})
	expectValues(t, svc, "E8", map[string]any{"TT_CWD_KEY": "cwd"})
}

func TestRegister_DefaultFileMissing(t *testing.T) {
	root := projectDir(t, "", false)
	t.Chdir(root)

	text, panicked := panicText(func() { registerWith(t, &ConfigModuleOptions{}) })
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "E8: a missing default file is not an error"))
	}
}

func TestRegister_ExplicitFileMissing(t *testing.T) {
	root := projectDir(t, "TT_DEFAULT=1\n", true)
	t.Chdir(root)
	missing := filepath.Join(root, "nope.env")

	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{EnvFilePaths: []string{missing}})
	})
	if !panicked || !strings.HasPrefix(text, "invalid config: "+missing+": ") {
		t.Error(test.DiffMessage(text, "invalid config: "+missing+": <os error>", "E8/E9: every path in ENVFilePaths must exist"))
	}
}

func TestRegister_ExplicitPathsReplaceDefault(t *testing.T) {
	root := projectDir(t, "TT_DEFAULT_ONLY=1\n", true)
	t.Chdir(root)
	explicit := writeEnv(t, "TT_EXPLICIT=1\n")

	svc := registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{explicit}})
	expectValues(t, svc, "E8", map[string]any{"TT_EXPLICIT": "1"})
	expectAbsent(t, svc, "E8: the default file is read only when ENVFilePaths is empty", "TT_DEFAULT_ONLY")
}

func TestRegister_IsIgnoreEnvFile(t *testing.T) {
	root := projectDir(t, "TT_IGN=1\nthis line is not valid\n", true)
	t.Chdir(root)

	var svc ConfigService
	text, panicked := panicText(func() {
		svc = registerWith(t, &ConfigModuleOptions{IsIgnoreEnvFile: true})
	})
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "E8: with IsIgnoreEnvFile no file is read, the default included"))
	}
	expectAbsent(t, svc, "E8", "TT_IGN")
}

func TestRegister_IsIgnoreEnvFileWithPaths(t *testing.T) {
	path := writeEnv(t, "TT_X=1\n")
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{IsIgnoreEnvFile: true, EnvFilePaths: []string{path}})
	})
	want := "invalid config: IsIgnoreEnvFile is set with EnvFilePaths"
	if !panicked || text != want {
		t.Error(test.DiffMessage(text, want, "E8/E9: IsIgnoreEnvFile together with ENVFilePaths"))
	}
}

func TestRegister_EnvironmentWins(t *testing.T) {
	t.Setenv("TT_PREC", "env")
	path := writeEnv(t, "TT_PREC=file\nTT_ONLY_FILE=file\n")

	svc := registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{path}})
	expectValues(t, svc, "E11", map[string]any{"TT_PREC": "env", "TT_ONLY_FILE": "file"})

	svc = registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{path}, IsOverride: true})
	expectValues(t, svc, "E11: IsOverride", map[string]any{"TT_PREC": "file"})
}

func TestRegister_FirstFileWins(t *testing.T) {
	first := writeEnv(t, "TT_K=first\nTT_ONLY_FIRST=1\n")
	second := writeEnv(t, "TT_K=second\nTT_ONLY_SECOND=2\n")

	svc := registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{first, second}})
	expectValues(t, svc, "E11", map[string]any{"TT_K": "first", "TT_ONLY_FIRST": "1", "TT_ONLY_SECOND": "2"})
}

func TestRegister_IsOverrideLastWins(t *testing.T) {
	first := writeEnv(t, "TT_K=first\n")
	second := writeEnv(t, "TT_K=second\n")
	loads := func() map[string]any { return map[string]any{"TT_K": "loads", "TT_L": "loads-only"} }

	svc := registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{first, second}, IsOverride: true})
	expectValues(t, svc, "E11", map[string]any{"TT_K": "second"})

	svc = registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{first}, Loads: []ConfigLoadFn{loads}})
	expectValues(t, svc, "E11: files before Loads", map[string]any{"TT_K": "first", "TT_L": "loads-only"})

	svc = registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{first}, Loads: []ConfigLoadFn{loads}, IsOverride: true})
	expectValues(t, svc, "E11: IsOverride, Loads after files", map[string]any{"TT_K": "loads"})

	later := func() map[string]any { return map[string]any{"TT_K": "later"} }
	svc = registerWith(t, &ConfigModuleOptions{Loads: []ConfigLoadFn{loads, later}})
	expectValues(t, svc, "E11: first Loads wins", map[string]any{"TT_K": "loads"})
	svc = registerWith(t, &ConfigModuleOptions{Loads: []ConfigLoadFn{loads, later}, IsOverride: true})
	expectValues(t, svc, "E11: last Loads wins with IsOverride", map[string]any{"TT_K": "later"})

	t.Setenv("TT_ENV_VS_LOADS", "env")
	svc = registerWith(t, &ConfigModuleOptions{Loads: []ConfigLoadFn{func() map[string]any { return map[string]any{"TT_ENV_VS_LOADS": "loads"} }}})
	expectValues(t, svc, "E11: the process environment beats Loads", map[string]any{"TT_ENV_VS_LOADS": "env"})
}

func TestRegister_ProcessEnvironmentIsVisible(t *testing.T) {
	t.Setenv("TT_VISIBLE", "from-os")
	svc := registerWith(t, &ConfigModuleOptions{IsIgnoreEnvFile: true})
	expectValues(t, svc, "E11", map[string]any{"TT_VISIBLE": "from-os"})
}

func TestRegister_DoesNotSetProcessEnv(t *testing.T) {
	path := writeEnv(t, "TT_NOSET=1\n")
	loads := func() map[string]any { return map[string]any{"TT_NOSET_LOADS": "1"} }
	svc := registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{path}, Loads: []ConfigLoadFn{loads}, IsOverride: true})
	expectValues(t, svc, "E11", map[string]any{"TT_NOSET": "1"})

	for _, key := range []string{"TT_NOSET", "TT_NOSET_LOADS"} {
		if value, ok := os.LookupEnv(key); ok {
			t.Error(test.DiffMessage(value, "unset", "E11: "+key+" must not be written to the process environment"))
		}
	}
}

func TestRegister_LoadsFlatten(t *testing.T) {
	loads := func() map[string]any {
		return map[string]any{
			"database": map[string]any{
				"port":  5432,
				"hosts": []any{"a", "b"},
			},
		}
	}
	svc := registerWith(t, &ConfigModuleOptions{IsIgnoreEnvFile: true, Loads: []ConfigLoadFn{loads}})
	expectValues(t, svc, "E12", map[string]any{"database.port": 5432, "database.hosts.0": "a", "database.hosts.1": "b"})
	if _, ok := svc.Get("database").(map[string]any); !ok {
		t.Error(test.DiffMessage(svc.Get("database"), "the parent map", "E12: the parent key is kept"))
	}
}

func TestRegister_LoadsTooDeep(t *testing.T) {
	for _, depth := range []int{40, 100_000} {
		build := func() map[string]any { return nested(depth) }
		text, panicked := panicText(func() {
			Register(&ConfigModuleOptions{IsIgnoreEnvFile: true, Loads: []ConfigLoadFn{
				func() map[string]any { return map[string]any{"ok": 1} },
				build,
			}})
		})
		want := "invalid config: Loads[1]: nested deeper than 32 levels"
		if !panicked || text != want {
			t.Error(test.DiffMessage(text, want, "E9/E12: nesting of "+strconv.Itoa(depth)+" levels"))
		}
	}
}

func TestRegister_LoadsCycleTooDeep(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{IsIgnoreEnvFile: true, Loads: []ConfigLoadFn{func() map[string]any { return cyclic }}})
	})
	want := "invalid config: Loads[0]: nested deeper than 32 levels"
	if !panicked || text != want {
		t.Error(test.DiffMessage(text, want, "E12: a map that contains itself is refused, not followed forever"))
	}
}

func TestRegister_LoadsShallowAccepted(t *testing.T) {
	text, panicked := panicText(func() {
		svc := registerWith(t, &ConfigModuleOptions{IsIgnoreEnvFile: true, Loads: []ConfigLoadFn{func() map[string]any { return nested(5) }}})
		expectValues(t, svc, "E12", map[string]any{"n.n.n.n.n.leaf": canary})
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "E12: 5 levels are fine"))
	}
}

func TestRegister_LoadsTooDeepNeverContainsValues(t *testing.T) {
	text, _ := panicText(func() {
		Register(&ConfigModuleOptions{IsIgnoreEnvFile: true, Loads: []ConfigLoadFn{func() map[string]any { return nested(50) }}})
	})
	if strings.Contains(text, canary) {
		t.Error(test.DiffMessage(text, "no value", "E10: the Loads panic leaked a value"))
	}
}

func TestRegister_HooksShareState(t *testing.T) {
	var order []string
	var seenByLater string
	hooks := []ConfigHookFn{
		func(s ConfigService) {
			order = append(order, "first")
			s.Set("TT_HOOK_A", "from-first")
		},
		func(s ConfigService) {
			order = append(order, "second")
			if v, ok := s.Get("TT_HOOK_A").(string); ok {
				seenByLater = v
			}
			s.Set("TT_HOOK_B", "from-second")
		},
		func(s ConfigService) {
			order = append(order, "third")
			if s.Get("TT_HOOK_B") != "from-second" {
				t.Error(test.DiffMessage(s.Get("TT_HOOK_B"), "from-second", "E14: a later hook sees what an earlier hook set"))
			}
		},
	}
	module := Register(&ConfigModuleOptions{IsIgnoreEnvFile: true, Hooks: hooks})

	if !slices.Equal(order, []string{"first", "second", "third"}) {
		t.Error(test.DiffMessage(order, []string{"first", "second", "third"}, "E14: hooks run in order, at Register"))
	}
	if seenByLater != "from-first" {
		t.Error(test.DiffMessage(seenByLater, "from-first", "E14: the second hook sees the first one's Set"))
	}

	app := core.New()
	app.Create(module)
	injected, ok := app.Get(ConfigService{}).(ConfigService)
	if !ok {
		t.Fatal(test.DiffMessage(app.Get(ConfigService{}), "a ConfigService", "E14: the module provides ConfigService"))
	}
	expectValues(t, injected, "E14: every injected ConfigService shares the state", map[string]any{"TT_HOOK_A": "from-first", "TT_HOOK_B": "from-second"})

	injected.Set("TT_AFTER_CREATE", "late")
	again, _ := app.Get(ConfigService{}).(ConfigService)
	expectValues(t, again, "E14: a Set through one copy is seen through another", map[string]any{"TT_AFTER_CREATE": "late"})
}

func TestRegister_OnInitAndGlobal(t *testing.T) {
	inits := 0
	module := Register(&ConfigModuleOptions{IsGlobal: true, IsIgnoreEnvFile: true, OnInit: func() { inits++ }})
	if !module.IsGlobal || module.OnInit == nil {
		t.Fatal(test.DiffMessage(module.IsGlobal, true, "IsGlobal and OnInit are copied to the module"))
	}
	core.New().Create(module)
	if inits != 1 {
		t.Error(test.DiffMessage(inits, 1, "OnInit runs once"))
	}
}

func TestRegister_NilOptions(t *testing.T) {
	root := projectDir(t, "", false)
	t.Chdir(root)
	if module := Register(nil); module == nil {
		t.Error(test.DiffMessage(nil, "a module", "Register(nil) returns a module"))
	}
}

func TestConfigState_FilesAndSortedKeys(t *testing.T) {
	t.Setenv("TT_STATE_ENV", "env-value")
	first := writeEnv(t, "TT_STATE_B=1\nTT_STATE_A=2\n")
	second := writeEnv(t, "TT_STATE_C=3\n")
	loads := func() map[string]any { return map[string]any{"tt_state_loaded": map[string]any{"x": 1}} }

	svc := registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{first, second}, Loads: []ConfigLoadFn{loads}})
	svc.Set("TT_STATE_SET", "later")

	state, ok := inspect.ConfigStateOf(svc)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "inspect.ConfigStateOf(ConfigService) must report true"))
	}
	if !slices.Equal(state.Files, []string{first, second}) {
		t.Error(test.DiffMessage(state.Files, []string{first, second}, "Files: the files read, in order"))
	}
	if !sort.StringsAreSorted(state.Keys) {
		t.Error(test.DiffMessage(state.Keys, "sorted", "Keys are sorted"))
	}
	for _, key := range []string{"TT_STATE_A", "TT_STATE_B", "TT_STATE_C", "TT_STATE_ENV", "TT_STATE_SET", "tt_state_loaded", "tt_state_loaded.x"} {
		if !slices.Contains(state.Keys, key) {
			t.Error(test.DiffMessage(state.Keys, "containing "+key, "Keys list every key of the state"))
		}
	}
}

func TestConfigState_NoFiles(t *testing.T) {
	svc := registerWith(t, &ConfigModuleOptions{IsIgnoreEnvFile: true})
	state, ok := inspect.ConfigStateOf(svc)
	if !ok || len(state.Files) != 0 {
		t.Error(test.DiffMessage(state.Files, []string{}, "Files is empty when no file was read"))
	}
}

func TestConfigState_NoValues(t *testing.T) {
	t.Setenv("TT_STATE_ENV_SECRET", canary+"-env")
	path := writeEnv(t, "TT_STATE_SECRET="+canary+"-file\n")
	loads := func() map[string]any { return map[string]any{"TT_STATE_LOADS": canary + "-loads"} }
	svc := registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{path}, Loads: []ConfigLoadFn{loads}})
	svc.Set("TT_STATE_SET_SECRET", canary+"-set")

	state, ok := inspect.ConfigStateOf(svc)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "ConfigStateOf reports true"))
	}
	for _, key := range state.Keys {
		if strings.Contains(key, canary) {
			t.Error(test.DiffMessage(key, "a key name", "the state never holds a value"))
		}
	}
	for _, key := range []string{"TT_STATE_ENV_SECRET", "TT_STATE_SECRET", "TT_STATE_LOADS", "TT_STATE_SET_SECRET"} {
		if !slices.Contains(state.Keys, key) {
			t.Error(test.DiffMessage(state.Keys, "containing "+key, "the key names are listed"))
		}
	}
	if strings.Contains(strings.Join(state.Files, "\n"), canary) {
		t.Error(test.DiffMessage(state.Files, "paths only", "the state never holds a value"))
	}
}

func TestConfigState_OfOtherTypeIsFalse(t *testing.T) {
	for _, other := range []any{nil, "config", 42, struct{}{}, &struct{ State int }{}} {
		state, ok := inspect.ConfigStateOf(other)
		if ok || len(state.Files) != 0 || len(state.Keys) != 0 {
			t.Error(test.DiffMessage(ok, false, "inspect.ConfigStateOf of a value that is not a ConfigService"))
		}
	}
}
