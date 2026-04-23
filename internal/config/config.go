package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type SchemaConfig struct {
	ProtoPaths          []string `json:"protoPaths"`
	EnvelopeMessageName string   `json:"envelopeMessageName"`
}

type Config struct {
	Device     string `json:"device"`
	ServerPort uint16 `json:"serverPort"`

	// ConnectionServer is a hostname (e.g. "dofus2-co-production.ankama-games.com")
	// or a literal IP. Resolved to IPs at startup; all TCP flows to/from those
	// IPs are decoded with the Connection schema.
	ConnectionServer string `json:"connectionServer"`

	// GameServerIPs is auto-populated by the sniffer at runtime: any remote IP
	// observed on the sniffed port that is not a connection-server IP gets
	// appended here and persisted so restarts pick up game traffic immediately.
	GameServerIPs []string `json:"gameServerIPs"`

	Connection SchemaConfig `json:"connection"`
	Game       SchemaConfig `json:"game"`

	MappingPaths []string `json:"mappingPaths"`
}

func Default() Config {
	return Config{
		ServerPort:       5555,
		ConnectionServer: "dofus2-co-production.ankama-games.com",
		GameServerIPs:    []string{},
		Connection:       SchemaConfig{ProtoPaths: []string{}, EnvelopeMessageName: "Message"},
		Game:             SchemaConfig{ProtoPaths: []string{}, EnvelopeMessageName: "Message"},
		MappingPaths:     []string{},
	}
}

func Load() (Config, error) {
	path, err := FilePath()
	if err != nil {
		return Default(), err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Default(), err
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	normalize(&cfg)
	return cfg, nil
}

func normalize(c *Config) {
	if c.Connection.ProtoPaths == nil {
		c.Connection.ProtoPaths = []string{}
	}
	if c.Game.ProtoPaths == nil {
		c.Game.ProtoPaths = []string{}
	}
	if c.Connection.EnvelopeMessageName == "" {
		c.Connection.EnvelopeMessageName = "Message"
	}
	if c.Game.EnvelopeMessageName == "" {
		c.Game.EnvelopeMessageName = "Message"
	}
	if c.MappingPaths == nil {
		c.MappingPaths = []string{}
	}
	if c.GameServerIPs == nil {
		c.GameServerIPs = []string{}
	}
}

func (c Config) Save() error {
	path, err := FilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
