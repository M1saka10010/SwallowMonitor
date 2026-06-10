package model

// Config is the SwallowMonitor server configuration.
type Config struct {
	Listen         string       `yaml:"listen"`
	PublicURL      string       `yaml:"publicUrl"`
	DBPath         string       `yaml:"dbPath"`
	RetentionDays  int          `yaml:"retentionDays"`
	OfflineTimeout int64        `yaml:"offlineTimeout"`
	GitHub         GitHubConfig `yaml:"github"`
	IsDebug        bool         `yaml:"isDebug"`
}

// GitHubConfig holds GitHub OAuth settings for the web panel.
type GitHubConfig struct {
	ClientID     string   `yaml:"clientId"`
	ClientSecret string   `yaml:"clientSecret"`
	AllowedUsers []string `yaml:"allowedUsers"`
}
