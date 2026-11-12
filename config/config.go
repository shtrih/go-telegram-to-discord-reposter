package config

import (
	"time"

	"gopkg.in/yaml.v2"
	"io/ioutil"
)

const DefaultRetention = "24h"

// DefaultMaxFileSizeMB mirrors Discord's default per-file upload limit.
const DefaultMaxFileSizeMB = 20

type Config struct {
	Database string `yaml:"database"`

	// Retention is the YAML duration string ("24h") limiting how long
	// Telegram posts stay editable; parsed into EditWindow.
	Retention string `yaml:"retention"`
	// EditWindow is the parsed Retention; 0 disables database cleanup.
	EditWindow time.Duration `yaml:"-"`

	// MaxFileSizeMB is the per-file upload limit in MiB; larger files are
	// skipped and offered for resend without them (0 falls back to default).
	MaxFileSizeMB int `yaml:"max_file_size_mb"`

	*Telegram `yaml:"telegram"`
	*Discord  `yaml:"discord"`

	*Proxy `yaml:"proxy"`
}

type Telegram struct {
	Token string `yaml:"token"`
	Debug string `yaml:"debug"`
}

type Discord struct {
	Token     string `yaml:"token"`
	ChannelID string `yaml:"channel_id"`
}

type Proxy struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

func NewConfig(p string) (*Config, error) {
	b, err := ioutil.ReadFile(p)
	if err != nil {
		return nil, err
	}

	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}

	if c.Retention == "" {
		c.Retention = DefaultRetention
	}
	if c.MaxFileSizeMB <= 0 {
		c.MaxFileSizeMB = DefaultMaxFileSizeMB
	}
	w, err := time.ParseDuration(c.Retention)
	if err != nil {
		return nil, err
	}
	c.EditWindow = w

	return &c, nil
}
