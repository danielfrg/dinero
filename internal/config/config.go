package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the on-disk Dinero configuration. Plaid fields are optional for
// read-only commands; Plaid operations validate their own requirements.
type Config struct {
	Timezone string   `toml:"timezone" json:"timezone"`
	Database Database `toml:"database" json:"database"`
	Plaid    Plaid    `toml:"plaid" json:"plaid"`
	Server   Server   `toml:"server" json:"server"`
}

type Database struct {
	ConnectionString string `toml:"connection_string" json:"-"`
}

type Plaid struct {
	ClientID        string            `toml:"client_id" json:"-"`
	Secret          string            `toml:"secret" json:"-"`
	Env             string            `toml:"env" json:"env"`
	Products        string            `toml:"products" json:"products"`
	Tokens          map[string]string `toml:"tokens" json:"-"`
	AccountIDToName map[string]string `toml:"account_id_to_name" json:"account_id_to_name"`
	ItemIDs         map[string]string `toml:"item_ids" json:"item_ids"`
}

type Server struct {
	Host           string   `toml:"host" json:"host"`
	Port           int      `toml:"port" json:"port"`
	AllowedHosts   []string `toml:"allowed_hosts" json:"allowed_hosts,omitempty"`
	AllowedOrigins []string `toml:"allowed_origins" json:"allowed_origins,omitempty"`
}

func Load(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, errors.New("invalid configuration TOML")
	}
	_, hasURL := os.LookupEnv("DINERO_DATABASE_URL")
	_, hasURLFile := os.LookupEnv("DINERO_DATABASE_URL_FILE")
	if cfg.Database.ConnectionString == "" && !hasURL && !hasURLFile {
		return Config{}, errors.New("database connection_string is required")
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "US/Central"
	}
	if cfg.Plaid.Tokens == nil {
		cfg.Plaid.Tokens = map[string]string{}
	}
	if cfg.Plaid.ItemIDs == nil {
		cfg.Plaid.ItemIDs = map[string]string{}
	}
	if cfg.Plaid.AccountIDToName == nil {
		cfg.Plaid.AccountIDToName = map[string]string{}
	}
	return cfg, nil
}

// DSN resolves deployment overrides and returns a driver-ready connection
// string. Callers must never put the returned value in output or errors.
func (c Config) DSN() (string, error) {
	url, urlSet := os.LookupEnv("DINERO_DATABASE_URL")
	file, fileSet := os.LookupEnv("DINERO_DATABASE_URL_FILE")
	if urlSet && fileSet {
		return "", errors.New("set only one database URL override")
	}
	if fileSet {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read database URL file: %w", err)
		}
		url = strings.TrimSpace(string(data))
	} else if !urlSet {
		url = c.Database.ConnectionString
	}
	return NormalizeDSN(url)
}

func NormalizeDSN(value string) (string, error) {
	switch {
	case strings.HasPrefix(value, "postgresql+psycopg://"):
		return "postgresql://" + strings.TrimPrefix(value, "postgresql+psycopg://"), nil
	case strings.HasPrefix(value, "postgresql://"), strings.HasPrefix(value, "postgres://"):
		return value, nil
	case strings.HasPrefix(value, "sqlite:///"):
		return strings.TrimPrefix(value, "sqlite:///"), nil
	default:
		return "", errors.New("unsupported database URL scheme; use postgresql or sqlite")
	}
}

// Redacted returns a printable view with aliases and account names but no
// credentials, access tokens, or database URL.
func (c Config) Redacted() string {
	aliases := make([]string, 0, len(c.Plaid.Tokens))
	for alias := range c.Plaid.Tokens {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	view := struct {
		Timezone string `json:"timezone"`
		Database string `json:"database"`
		Plaid    struct {
			Env      string            `json:"env,omitempty"`
			Products string            `json:"products,omitempty"`
			Aliases  []string          `json:"aliases"`
			Accounts map[string]string `json:"account_id_to_name"`
		} `json:"plaid"`
		Server Server `json:"server"`
	}{Timezone: c.Timezone, Database: "[REDACTED]", Server: c.Server}
	view.Plaid.Env = c.Plaid.Env
	view.Plaid.Products = c.Plaid.Products
	view.Plaid.Aliases = aliases
	view.Plaid.Accounts = c.Plaid.AccountIDToName
	data, _ := json.MarshalIndent(view, "", "  ")
	return string(data)
}
