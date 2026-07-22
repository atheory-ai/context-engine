package config

import (
	"runtime"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadAcceptsDocumentedPluginInstalledShape(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("data", map[string]any{"dir": t.TempDir()})
	viper.Set("plugins", map[string]any{
		"installed": []map[string]any{
			{"path": "./plugins/php-language.wasm"},
			{"path": "./plugins/wordpress-conventions.wasm", "config": map[string]any{"framework": "wordpress"}},
		},
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Plugins) != 2 {
		t.Fatalf("len(cfg.Plugins) = %d, want 2", len(cfg.Plugins))
	}
	if got := cfg.Plugins[0].Path; got != "./plugins/php-language.wasm" {
		t.Errorf("cfg.Plugins[0].Path = %q", got)
	}
	if got := cfg.Plugins[1].Config["framework"]; got != "wordpress" {
		t.Errorf("cfg.Plugins[1].Config[framework] = %v", got)
	}
}

func TestDefaultIndexWorkersCapsOversubscription(t *testing.T) {
	want := min(max(1, runtime.NumCPU()*2), 12)
	if got := defaultIndexWorkers(); got != want {
		t.Fatalf("defaultIndexWorkers() = %d, want %d", got, want)
	}
}

func TestLoadRetainsBarePluginListCompatibility(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("data", map[string]any{"dir": t.TempDir()})
	viper.Set("plugins", []map[string]any{{"path": "./plugins/existing.wasm"}})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Path != "./plugins/existing.wasm" {
		t.Fatalf("cfg.Plugins = %#v, want existing bare-list plugin", cfg.Plugins)
	}
}

func TestLoadExtractsPluginActivationFromDocumentedPluginsBlock(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("data", map[string]any{"dir": t.TempDir()})
	viper.Set("plugins", map[string]any{
		"profile":   "wordpress",
		"enabled":   []string{"com.example.php", "com.example.wordpress"},
		"installed": []map[string]any{{"path": "./plugins/php.wasm"}},
	})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PluginActivation.Profile != "wordpress" {
		t.Fatalf("profile = %q", cfg.PluginActivation.Profile)
	}
	if len(cfg.PluginActivation.Enabled) != 2 || cfg.PluginActivation.Enabled[1] != "com.example.wordpress" {
		t.Fatalf("enabled = %#v", cfg.PluginActivation.Enabled)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Path != "./plugins/php.wasm" {
		t.Fatalf("installed = %#v", cfg.Plugins)
	}
}

func TestLoadHonorsGlobalDataDirOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	projectDir := t.TempDir()
	overrideDir := t.TempDir()
	viper.Set("data", map[string]any{"dir": projectDir})
	viper.Set("data_dir", overrideDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DataDir != overrideDir {
		t.Errorf("cfg.DataDir = %q, want global override %q", cfg.DataDir, overrideDir)
	}
}
