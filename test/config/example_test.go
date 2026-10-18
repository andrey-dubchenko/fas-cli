package config_test

import (
	"fas-cli/internal/config"
	"strings"
	"testing"
)

// The example config must stay loadable, secure by default and free of real hosts.
func TestExampleConfigLoads(t *testing.T) {
	cfg, err := config.Load("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Environments) == 0 || cfg.InsecureSkipVerify {
		t.Fatalf("unexpected example config: %+v", cfg)
	}
	for _, e := range cfg.Environments {
		if !strings.Contains(e.FlinkAdmin, "example.com") || !strings.Contains(e.OpenSearch, "example.com") {
			t.Errorf("environment %s has a non-placeholder URL: %+v", e.Name, e)
		}
	}
}
