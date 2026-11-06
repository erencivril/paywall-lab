package remoteconfig

import (
	"fmt"
	"strings"
)

// PaywallProduct is one purchasable plan as shown on the paywall.
type PaywallProduct struct {
	ProductID    string `json:"product_id"`
	Title        string `json:"title"`
	Period       string `json:"period"`
	PriceCents   int    `json:"price_cents"`
	Currency     string `json:"currency"`
	DisplayPrice string `json:"display_price"`
	Highlighted  bool   `json:"highlighted"`
}

// Paywall is what the client needs to draw the paywall for one user.
type Paywall struct {
	Country       string           `json:"country"`
	Tier          string           `json:"tier"`
	Variant       string           `json:"variant"`
	ConfigVersion int              `json:"config_version"`
	Title         string           `json:"title"`
	CTAText       string           `json:"cta_text"`
	TrialDays     int              `json:"trial_days"`
	Products      []PaywallProduct `json:"products"`
}

// Paywall builds the paywall for a country and user. The country must already
// be validated and upper-cased.
func (c *Config) Paywall(country, userID string) Paywall {
	tier := c.TierFor(country)
	variantName := c.VariantFor(userID)
	variant := c.Variants[variantName]
	t := c.Tiers[tier]

	products := make([]PaywallProduct, 0, len(c.Products))
	for _, p := range c.Products {
		cents := t.Prices[p.ProductID]
		products = append(products, PaywallProduct{
			ProductID:    p.ProductID,
			Title:        p.Title,
			Period:       p.Period,
			PriceCents:   cents,
			Currency:     t.Currency,
			DisplayPrice: FormatPrice(cents, t.Currency),
			Highlighted:  p.ProductID == variant.Highlight,
		})
	}

	return Paywall{
		Country:       country,
		Tier:          tier,
		Variant:       variantName,
		ConfigVersion: c.Version,
		Title:         variant.Title,
		CTAText:       strings.ReplaceAll(variant.CTATemplate, "{trial_days}", fmt.Sprint(t.TrialDays)),
		TrialDays:     t.TrialDays,
		Products:      products,
	}
}

// FormatPrice renders minor units as a display string, e.g. 999 USD -> "$9.99".
func FormatPrice(cents int, currency string) string {
	symbol := currency + " "
	if currency == "USD" {
		symbol = "$"
	}
	return fmt.Sprintf("%s%d.%02d", symbol, cents/100, cents%100)
}
