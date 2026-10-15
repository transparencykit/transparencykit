// Package config loads vitrine.json.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/exoscale/vitrine/internal/geo"
	"github.com/exoscale/vitrine/internal/store"
)

// Provider identifies the publishing organisation.
type Provider struct {
	Name        string `json:"name"`
	LegalEntity string `json:"legal_entity"`
	Country     string `json:"country"`
	Contact     string `json:"contact"`
	Website     string `json:"website,omitempty"`
	ServiceType string `json:"service_type,omitempty"` // DSA "Service" column
}

// Config is the operator configuration.
type Config struct {
	Provider          Provider         `json:"provider"`
	HomeJurisdictions []string         `json:"home_jurisdictions"`
	Ledger            string           `json:"ledger"`
	Public            string           `json:"public"`
	PublicBaseURL     string           `json:"public_base_url,omitempty"`
	SigningKey        string           `json:"signing_key"`
	MinCount          int              `json:"min_count"`
	LedgerRetention   *store.Retention `json:"ledger_retention,omitempty"`

	dir string
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w (run `vitrine init`?)", err)
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	abs, _ := filepath.Abs(path)
	c.dir = filepath.Dir(abs)
	if len(c.HomeJurisdictions) == 0 {
		c.HomeJurisdictions = []string{"CH", "EU", "EEA"}
	}
	if c.MinCount == 0 {
		c.MinCount = 3
	}
	if c.Provider.ServiceType == "" {
		c.Provider.ServiceType = "provider of hosting services"
	}
	return &c, c.Validate()
}

// Validate checks required fields.
func (c *Config) Validate() error {
	var errs []string
	if c.Provider.Name == "" {
		errs = append(errs, "provider.name is required")
	}
	if c.Provider.Country != "" {
		if err := geo.ValidCountry(c.Provider.Country); err != nil {
			errs = append(errs, "provider.country: "+err.Error())
		}
	}
	if c.Ledger == "" {
		errs = append(errs, "ledger store URL is required")
	}
	if c.MinCount < 1 {
		errs = append(errs, "min_count must be >= 1")
	}
	if _, err := geo.Expand(c.HomeJurisdictions); err != nil {
		errs = append(errs, "home_jurisdictions: "+err.Error())
	}
	if r := c.LedgerRetention; r != nil {
		m := strings.ToUpper(r.Mode)
		if (m != "GOVERNANCE" && m != "COMPLIANCE") || r.Days < 1 {
			errs = append(errs, "ledger_retention needs mode GOVERNANCE|COMPLIANCE and days >= 1")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Resolve makes a relative path (or file:// URL) relative to the config dir.
func (c *Config) Resolve(p string) string {
	if strings.HasPrefix(p, "file://") {
		rest := strings.TrimPrefix(p, "file://")
		if !filepath.IsAbs(rest) {
			return "file://" + filepath.Join(c.dir, rest)
		}
		return p
	}
	if strings.Contains(p, "://") || filepath.IsAbs(p) || p == "" {
		return p
	}
	return filepath.Join(c.dir, p)
}

// Home returns the expanded home-jurisdiction set.
func (c *Config) Home() geo.Set {
	s, _ := geo.Expand(c.HomeJurisdictions)
	return s
}

// Example is the template written by `vitrine init`.
const Example = `{
  "provider": {
    "name": "Example Cloud SA",
    "legal_entity": "Example Cloud SA",
    "country": "CH",
    "contact": "transparency@example.ch",
    "website": "https://example.ch",
    "service_type": "provider of hosting services"
  },
  "home_jurisdictions": ["CH", "EU", "EEA"],
  "ledger": "file://./ledger",
  "public": "file://./public",
  "public_base_url": "",
  "signing_key": "vitrine.key",
  "min_count": 3
}
`
