package config

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/dotenv"
)

type (
	ConfigLoadFn   = func() map[string]any
	ConfigHookFn   = func(ConfigService)
	ConfigOnInitFn = func()
)

type ConfigModuleOptions struct {
	IsGlobal          bool
	IsIgnoreEnvFile   bool
	IsOverride        bool
	IsExpandVariables bool
	EnvFilePaths      []string
	Loads             []ConfigLoadFn
	Hooks             []ConfigHookFn
	OnInit            ConfigOnInitFn
}

func loadConfigOptions(opts *ConfigModuleOptions) *ConfigModuleOptions {
	if opts == nil {
		opts = &ConfigModuleOptions{}
	}
	if opts.IsIgnoreEnvFile && len(opts.EnvFilePaths) > 0 {
		failConfig("IsIgnoreEnvFile is set with EnvFilePaths")
	}

	envFilePaths := opts.EnvFilePaths
	if len(envFilePaths) == 0 && !opts.IsIgnoreEnvFile {
		defaultPath := defaultENVFilePath()
		if _, err := os.Stat(defaultPath); !errors.Is(err, fs.ErrNotExist) {
			envFilePaths = []string{defaultPath}
		}
	}

	return &ConfigModuleOptions{
		IsGlobal:          opts.IsGlobal,
		IsIgnoreEnvFile:   opts.IsIgnoreEnvFile,
		IsOverride:        opts.IsOverride,
		IsExpandVariables: opts.IsExpandVariables,
		EnvFilePaths:      envFilePaths,
		Loads:             opts.Loads,
		Hooks:             opts.Hooks,
		OnInit:            opts.OnInit,
	}
}

func defaultENVFilePath() string {
	dir, err := findRootDir("go.mod")
	if err != nil {
		dir, _ = os.Getwd()
	}
	return filepath.Join(dir, ".env")
}

func loadOSEnv() map[string]any {
	env := os.Environ()
	osENVMap := make(map[string]any, len(env))
	for _, v := range env {
		if idx := strings.IndexByte(v, '='); idx > 0 {
			osENVMap[v[:idx]] = v[idx+1:]
		}
	}
	return osENVMap
}

func mergeInto(valuesByKey map[string]any, isOverride bool, envs ...map[string]any) {
	for _, env := range envs {
		for key, value := range env {
			if _, ok := valuesByKey[key]; ok && !isOverride {
				continue
			}
			valuesByKey[key] = value
		}
	}
}

func Register(opts *ConfigModuleOptions) *core.Module {
	configOptions := loadConfigOptions(opts)
	envs := make([]map[string]any, 0, len(configOptions.EnvFilePaths)+len(configOptions.Loads))

	for _, path := range configOptions.EnvFilePaths {
		dotENVMap, err := dotenv.Load(path, configOptions.IsExpandVariables)
		if err != nil {
			panic(color.FmtRed(err.Error()))
		}
		envs = append(envs, dotENVMap)
	}

	for i, loadCustomENV := range configOptions.Loads {
		customENVMap, ok := flatten(maps.Clone(loadCustomENV()), nil, "").(map[string]any)
		if !ok {
			failConfig("Loads[" + strconv.Itoa(i) + "]: nested deeper than 32 levels")
		}
		envs = append(envs, customENVMap)
	}

	valuesByKey := loadOSEnv()
	mergeInto(valuesByKey, configOptions.IsOverride, envs...)
	configService := ConfigService{State: newConfigState(configOptions.EnvFilePaths, valuesByKey)}

	for _, hookFn := range configOptions.Hooks {
		hookFn(configService)
	}

	module := core.NewModuleBuilder().
		Providers(configService).
		Build()

	module.IsGlobal = configOptions.IsGlobal
	module.OnInit = configOptions.OnInit
	return module
}
