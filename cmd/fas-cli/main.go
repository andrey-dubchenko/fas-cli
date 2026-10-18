package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"fas-cli/internal/config"
	"fas-cli/internal/ui"
)

const (
	appName = "fas-cli"
	authors = "Andrey Dubchenko, Stanislav Agarkov"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

func main() {
	module := flag.String("module", "", "Module name (default: from config)")
	configPath := flag.String("config", "", "Config file path")
	envName := flag.String("env", "", "Initial environment (default: from config)")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s\n", appName, version)
		fmt.Printf("Authors: %s\n", authors)
		fmt.Printf("GitHub: https://github.com/agarkoff | https://github.com/andrey-dubchenko\n")
		os.Exit(0)
	}

	// Find config file
	cfgPath := *configPath
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	if cfgPath == "" {
		// Search in multiple locations
		searchPaths := []string{
			"config.json",
		}
		if exeDir != "" {
			searchPaths = append(searchPaths, filepath.Join(exeDir, "config.json"))
		}

		// Add current working directory
		if cwd, err := os.Getwd(); err == nil {
			searchPaths = append(searchPaths, filepath.Join(cwd, "config.json"))
		}

		// Find first existing config
		for _, p := range searchPaths {
			if _, err := os.Stat(p); err == nil {
				cfgPath = p
				break
			}
		}

		if cfgPath == "" {
			// No config.json anywhere: fall through to config.Load below,
			// which creates a default one at this path rather than making
			// a fresh install fail to start.
			if exeDir != "" {
				cfgPath = filepath.Join(exeDir, "config.json")
			} else {
				cfgPath = "config.json"
			}
		}
	}

	_, statErr := os.Stat(cfgPath)
	configIsNew := os.IsNotExist(statErr)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}
	if configIsNew {
		fmt.Fprintf(os.Stderr, "No config.json found — created a default one at %s\n", cfgPath)
		fmt.Fprintln(os.Stderr, "Edit it (or press 'c' in the app) to set your real environments.")
	}

	// Determine module. An empty moduleName is valid here: it means "no
	// module filter", i.e. show jobs from every module. That's the natural
	// outcome when -module isn't passed and the config has no modules list
	// (and thus no DefaultModule/Modules[0] to fall back to).
	moduleName := *module
	if moduleName == "" {
		moduleName = cfg.DefaultModule
		// A stale or placeholder defaultModule (e.g. from an older generated
		// config) that isn't in the declared list falls back to the first
		// module instead of aborting startup. An explicit -module is still
		// validated strictly below.
		if len(cfg.Modules) > 0 && !slices.Contains(cfg.Modules, moduleName) {
			moduleName = cfg.Modules[0]
		}
	}

	// Validate module, but only when the config actually declares a fixed
	// list — with no list, any module name (or none) is acceptable.
	if len(cfg.Modules) > 0 && moduleName != "" {
		moduleFound := false
		for _, m := range cfg.Modules {
			if m == moduleName {
				moduleFound = true
				break
			}
		}
		if !moduleFound {
			fmt.Fprintf(os.Stderr, "Unknown module: %s\n", moduleName)
			fmt.Fprintf(os.Stderr, "Available modules: %s\n", strings.Join(cfg.Modules, ", "))
			os.Exit(1)
		}
	}

	// Find initial environment
	envIdx := 0
	if *envName != "" {
		// User specified environment
		found := false
		for i, e := range cfg.Environments {
			if strings.EqualFold(e.Name, *envName) {
				envIdx = i
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "Unknown environment: %s\n", *envName)
			os.Exit(1)
		}
		if cfg.Environments[envIdx].Disabled {
			fmt.Fprintf(os.Stderr, "Environment is disabled: %s\n", cfg.Environments[envIdx].Name)
			os.Exit(1)
		}
	} else if cfg.DefaultEnv != "" && len(cfg.Environments) > 0 {
		// Use default from config. With no environments at all, DefaultEnv
		// naming one that can't exist isn't a fatal misconfiguration here —
		// the app itself reports "no environments configured" once it starts.
		// A defaultEnv that matches no environment (stale or placeholder)
		// falls back to the first one rather than aborting startup.
		for i, e := range cfg.Environments {
			if strings.EqualFold(e.Name, cfg.DefaultEnv) {
				envIdx = i
				break
			}
		}
	}

	// A disabled environment must not be the one we start on by default:
	// move to the first enabled one, if any.
	if *envName == "" && len(cfg.Environments) > 0 && cfg.Environments[envIdx].Disabled {
		for i, e := range cfg.Environments {
			if !e.Disabled {
				envIdx = i
				break
			}
		}
	}

	a := ui.NewApp(moduleName, cfg, envIdx, cfgPath)
	if err := a.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
