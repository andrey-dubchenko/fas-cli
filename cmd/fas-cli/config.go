package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config types

type Environment struct {
	Name       string `json:"name"`
	FlinkAdmin string `json:"flinkAdmin"`
	OpenSearch string `json:"opensearch"`
	Disabled   bool   `json:"disabled"`
}

type Config struct {
	Modules       []string      `json:"modules"`       // available modules; empty means no filtering
	DefaultModule string        `json:"defaultModule"` // module used on startup
	DefaultEnv    string        `json:"defaultEnv"`    // environment name used on startup
	Environments  []Environment `json:"environments"`
	Endpoints     Endpoints     `json:"endpoints"` // Flink Admin API path templates
}

// Endpoints holds the Flink Admin API path templates the app calls.
// "{name}" and "{version}" placeholders are substituted with the schema's
// name and version. These match the "schema-v2" tag in Flink Admin's own
// OpenAPI doc (api-docs.json) by default; if a given instance runs an older
// or differently-versioned API (e.g. the deprecated "v1" paths), point these
// at whatever that instance actually exposes instead of patching the code.
type Endpoints struct {
	SchemaList   string `json:"schemaList,omitempty"`   // GET, paginated with page_num/page_size
	SchemaStop   string `json:"schemaStop,omitempty"`   // POST
	SchemaStart  string `json:"schemaStart,omitempty"`  // POST, body {"startArg":[]}
	SchemaDelete string `json:"schemaDelete,omitempty"` // DELETE, ?deleteIndex=false appended
}

func defaultEndpoints() Endpoints {
	return Endpoints{
		SchemaList:   "/v2/schema/short",
		SchemaStop:   "/v2/schema/{name}/{version}/stop",
		SchemaStart:  "/v2/schema/{name}/{version}/start",
		SchemaDelete: "/v2/schema/{name}/{version}",
	}
}

// fillDefaults replaces any blank template with its v2 default, so
// config.json files written before Endpoints existed (or with some fields
// left out) keep working unchanged.
func (e *Endpoints) fillDefaults() {
	d := defaultEndpoints()
	if e.SchemaList == "" {
		e.SchemaList = d.SchemaList
	}
	if e.SchemaStop == "" {
		e.SchemaStop = d.SchemaStop
	}
	if e.SchemaStart == "" {
		e.SchemaStart = d.SchemaStart
	}
	if e.SchemaDelete == "" {
		e.SchemaDelete = d.SchemaDelete
	}
}

// defaultConfig is written to disk the first time the app runs with no
// config.json. It's deliberately a blank slate rather than a set of
// plausible-looking example environments: fake URLs that don't correspond
// to any real deployment are more likely to confuse a new user than an
// honest "nothing configured yet". Endpoints are left blank on disk too;
// loadConfig fills in the real defaults in memory via fillDefaults.
func defaultConfig() Config {
	return Config{
		Modules:       []string{},
		DefaultModule: "DEFAULT",
		DefaultEnv:    "DEFAULT",
		Environments:  []Environment{},
		Endpoints:     Endpoints{},
	}
}

func saveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Generate default config if file doesn't exist
			cfg := defaultConfig()
			if err := saveConfig(path, cfg); err != nil {
				return Config{}, fmt.Errorf("create default config: %w", err)
			}
			// The file on disk stays a blank slate (see defaultConfig), but
			// the in-memory Endpoints still need real values so schema
			// operations work the moment an environment is added, without
			// requiring a restart to pick up fillDefaults from a re-read.
			cfg.Endpoints.fillDefaults()
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	// Both Modules and Environments may be empty. Empty Modules means "don't
	// filter by module" (show every job). Empty Environments means there's
	// nothing to connect to yet; the app shows that plainly instead of
	// crashing, so a bare-bones config like this is a valid starting point:
	//
	//	{"modules": [], "environments": [], "endpoints": {}}
	cfg.Endpoints.fillDefaults()
	return cfg, nil
}
