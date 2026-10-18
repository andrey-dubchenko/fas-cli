package config_test

import (
	"fas-cli/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigTrimsSlashesAndFillsDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	data := `{"environments":[{"name":"DEV","flinkAdmin":"http://f/","opensearch":"http://o//"}]}`
	if err := os.WriteFile(p, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	e := cfg.Environments[0]
	if e.FlinkAdmin != "http://f" || e.OpenSearch != "http://o" {
		t.Fatalf("urls not trimmed: %+v", e)
	}
	if cfg.Endpoints.SchemaList == "" || cfg.InsecureSkipVerify {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
