package config

// ServeConfig holds all top-level operational flags for `pour serve`.
// Parsed by Kong; env-var names are in the struct tags.
//
// POUR_MNEMONIC and POUR_ADMIN_TOKEN are intentionally absent — both are
// secrets read via os.Getenv only. CLI flags appear in process listings and
// shell history; secrets must never be exposed that way (§2.13).
type ServeConfig struct {
	Listen     string `kong:"default=':8080',env='POUR_LISTEN',help='Address to listen on.'"`
	ConfigFile string `kong:"default='chains.yml',env='POUR_CONFIG',help='Path to chains.yml.'"`
	DBPath     string `kong:"default='pour.db',env='POUR_DB_PATH',help='Path to SQLite database.'"`
	LogLevel   string `kong:"default='info',env='POUR_LOG_LEVEL',help='Log level (debug|info|warn|error).'"`
	Metrics    bool   `kong:"env='POUR_METRICS',help='Enable Prometheus metrics endpoint.'"`
	NoUI       bool   `kong:"name='no-ui',env='POUR_NO_UI',help='Disable embedded web UI.'"`
}
