// Package remoteconfig loads a Firebase Remote Config style JSON file and
// answers pricing and paywall questions from it: which tier a country is in,
// what a product costs there, and which A/B variant a user sees.
package remoteconfig

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Tier names used in the config and in analytics events.
const (
	Tier1   = "tier1"
	ExTier1 = "ex_tier1"
)

var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

type Tier struct {
	Countries []string       `json:"countries"`
	Currency  string         `json:"currency"`
	TrialDays int            `json:"trial_days"`
	Prices    map[string]int `json:"prices"` // product_id -> price in minor units
}

type Product struct {
	ProductID  string `json:"product_id"`
	Title      string `json:"title"`
	Period     string `json:"period"`
	PeriodDays int    `json:"period_days"`
}

type SplitEntry struct {
	Variant string `json:"variant"`
	Weight  int    `json:"weight"`
}

type Experiment struct {
	Name  string       `json:"name"`
	Split []SplitEntry `json:"split"`
}

type Variant struct {
	Title       string `json:"title"`
	CTATemplate string `json:"cta_template"`
	Highlight   string `json:"highlight"`
}

type Config struct {
	Version     int                `json:"version"`
	DefaultTier string             `json:"default_tier"`
	Tiers       map[string]Tier    `json:"tiers"`
	Products    []Product          `json:"products"`
	Experiment  Experiment         `json:"experiment"`
	Variants    map[string]Variant `json:"variants"`

	countryTier map[string]string
}

// Load reads and validates the config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw)
}

// Parse decodes and validates config JSON.
func Parse(raw []byte) (*Config, error) {
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &c, nil
}

func (c *Config) validate() error {
	if _, ok := c.Tiers[c.DefaultTier]; !ok {
		return fmt.Errorf("default_tier %q is not defined in tiers", c.DefaultTier)
	}
	if len(c.Products) == 0 {
		return fmt.Errorf("no products defined")
	}
	for _, p := range c.Products {
		if p.ProductID == "" || p.PeriodDays <= 0 {
			return fmt.Errorf("product %q needs an id and a positive period_days", p.ProductID)
		}
	}

	c.countryTier = make(map[string]string)
	for name, t := range c.Tiers {
		if t.Currency == "" || t.TrialDays < 0 {
			return fmt.Errorf("tier %q needs a currency and a non-negative trial_days", name)
		}
		for _, p := range c.Products {
			price, ok := t.Prices[p.ProductID]
			if !ok || price <= 0 {
				return fmt.Errorf("tier %q has no positive price for %q", name, p.ProductID)
			}
		}
		for _, cc := range t.Countries {
			if !countryPattern.MatchString(cc) {
				return fmt.Errorf("tier %q: bad country code %q", name, cc)
			}
			if other, dup := c.countryTier[cc]; dup {
				return fmt.Errorf("country %s is listed in both %q and %q", cc, other, name)
			}
			c.countryTier[cc] = name
		}
	}

	total := 0
	for _, s := range c.Experiment.Split {
		if _, ok := c.Variants[s.Variant]; !ok {
			return fmt.Errorf("split references unknown variant %q", s.Variant)
		}
		if s.Weight <= 0 {
			return fmt.Errorf("variant %q needs a positive weight", s.Variant)
		}
		total += s.Weight
	}
	if total != 100 {
		return fmt.Errorf("split weights add up to %d, want 100", total)
	}
	for name, v := range c.Variants {
		if _, ok := c.Product(v.Highlight); !ok {
			return fmt.Errorf("variant %q highlights unknown product %q", name, v.Highlight)
		}
	}
	return nil
}

// TierFor returns the tier name for a two-letter country code. Countries that
// are not listed fall back to the default tier.
func (c *Config) TierFor(country string) string {
	if t, ok := c.countryTier[strings.ToUpper(country)]; ok {
		return t
	}
	return c.DefaultTier
}

func (c *Config) Product(id string) (Product, bool) {
	for _, p := range c.Products {
		if p.ProductID == id {
			return p, true
		}
	}
	return Product{}, false
}

// Price returns the price in minor units and the currency for a product in a tier.
func (c *Config) Price(productID, tier string) (cents int, currency string, ok bool) {
	t, found := c.Tiers[tier]
	if !found {
		return 0, "", false
	}
	cents, ok = t.Prices[productID]
	return cents, t.Currency, ok
}

func (c *Config) TrialDays(tier string) int {
	return c.Tiers[tier].TrialDays
}

// VariantFor assigns a user to a variant. The bucket (0-99) comes from a
// SHA-256 hash of "<experiment>:<user_id>", so the same user always gets the
// same variant and a different experiment name reshuffles everyone.
func (c *Config) VariantFor(userID string) string {
	sum := sha256.Sum256([]byte(c.Experiment.Name + ":" + userID))
	bucket := int(binary.BigEndian.Uint32(sum[:4]) % 100)
	upper := 0
	for _, s := range c.Experiment.Split {
		upper += s.Weight
		if bucket < upper {
			return s.Variant
		}
	}
	return c.Experiment.Split[len(c.Experiment.Split)-1].Variant
}
