package remoteconfig

import (
	"fmt"
	"strings"
	"testing"
)

const configPath = "../../config/remote_config.json"

func loadShipped(t *testing.T) *Config {
	t.Helper()
	c, err := Load(configPath)
	if err != nil {
		t.Fatalf("load shipped config: %v", err)
	}
	return c
}

func TestTierFor(t *testing.T) {
	c := loadShipped(t)
	tests := []struct {
		country string
		want    string
	}{
		{"US", Tier1},
		{"us", Tier1}, // lookup is case-insensitive
		{"GB", Tier1},
		{"JP", Tier1},
		{"BR", ExTier1},
		{"IN", ExTier1},
		{"TR", ExTier1},
		{"ZZ", ExTier1}, // unknown codes use the default tier
	}
	for _, tt := range tests {
		t.Run(tt.country, func(t *testing.T) {
			if got := c.TierFor(tt.country); got != tt.want {
				t.Errorf("TierFor(%q) = %q, want %q", tt.country, got, tt.want)
			}
		})
	}
}

func TestPrice(t *testing.T) {
	c := loadShipped(t)
	tests := []struct {
		product, tier string
		cents         int
		ok            bool
	}{
		{"premium_monthly", Tier1, 999, true},
		{"premium_annual", Tier1, 5999, true},
		{"premium_monthly", ExTier1, 499, true},
		{"premium_annual", ExTier1, 2999, true},
		{"premium_weekly", Tier1, 0, false},
		{"premium_monthly", "tier9", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.product+"/"+tt.tier, func(t *testing.T) {
			cents, cur, ok := c.Price(tt.product, tt.tier)
			if ok != tt.ok || cents != tt.cents {
				t.Fatalf("Price = (%d, %q, %v), want (%d, _, %v)", cents, cur, ok, tt.cents, tt.ok)
			}
		})
	}
}

func TestVariantForIsDeterministic(t *testing.T) {
	c := loadShipped(t)
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("user-%d", i)
		first := c.VariantFor(id)
		for j := 0; j < 5; j++ {
			if got := c.VariantFor(id); got != first {
				t.Fatalf("variant for %s changed from %s to %s", id, first, got)
			}
		}
	}
}

func TestVariantSplitIsRoughlyEven(t *testing.T) {
	c := loadShipped(t)
	counts := map[string]int{}
	const users = 10000
	for i := 0; i < users; i++ {
		counts[c.VariantFor(fmt.Sprintf("load-user-%d", i))]++
	}
	for variant, n := range counts {
		share := float64(n) / users
		if share < 0.45 || share > 0.55 {
			t.Errorf("variant %s got %.1f%% of users, want about 50%%", variant, share*100)
		}
	}
	if len(counts) != 2 {
		t.Errorf("expected 2 variants, got %v", counts)
	}
}

func TestPaywallUsesTierPricesAndTrial(t *testing.T) {
	c := loadShipped(t)

	us := c.Paywall("US", "u1")
	if us.Tier != Tier1 || us.TrialDays != 7 {
		t.Errorf("US paywall tier=%s trial=%d", us.Tier, us.TrialDays)
	}
	if got := us.Products[0].DisplayPrice; got != "$9.99" {
		t.Errorf("US monthly price = %s, want $9.99", got)
	}

	br := c.Paywall("BR", "u1")
	if br.Tier != ExTier1 || br.TrialDays != 3 {
		t.Errorf("BR paywall tier=%s trial=%d", br.Tier, br.TrialDays)
	}
	if got := br.Products[0].DisplayPrice; got != "$4.99" {
		t.Errorf("BR monthly price = %s, want $4.99", got)
	}
	if !strings.Contains(br.CTAText, "3") {
		t.Errorf("CTA %q should mention the 3 trial days", br.CTAText)
	}
}

func TestPaywallHighlightsOneProduct(t *testing.T) {
	c := loadShipped(t)
	for i := 0; i < 20; i++ {
		pw := c.Paywall("US", fmt.Sprintf("u%d", i))
		n := 0
		for _, p := range pw.Products {
			if p.Highlighted {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("variant %s highlights %d products, want 1", pw.Variant, n)
		}
	}
}

func TestParseRejectsBadConfig(t *testing.T) {
	good := `{
	  "version": 1, "default_tier": "a",
	  "tiers": {"a": {"countries": ["US"], "currency": "USD", "trial_days": 3, "prices": {"p": 100}}},
	  "products": [{"product_id": "p", "title": "P", "period": "month", "period_days": 30}],
	  "experiment": {"name": "e", "split": [{"variant": "v", "weight": 100}]},
	  "variants": {"v": {"title": "T", "cta_template": "go", "highlight": "p"}}
	}`
	if _, err := Parse([]byte(good)); err != nil {
		t.Fatalf("baseline config should parse: %v", err)
	}

	tests := []struct {
		name, from, to, wantErr string
	}{
		{"unknown default tier", `"default_tier": "a"`, `"default_tier": "zzz"`, "default_tier"},
		{"missing price", `"prices": {"p": 100}`, `"prices": {}`, "no positive price"},
		{"bad country", `["US"]`, `["usa"]`, "bad country code"},
		{"weights not 100", `"weight": 100`, `"weight": 60`, "want 100"},
		{"unknown variant in split", `"variant": "v"`, `"variant": "nope"`, "unknown variant"},
		{"unknown highlight", `"highlight": "p"`, `"highlight": "q"`, "unknown product"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := strings.Replace(good, tt.from, tt.to, 1)
			_, err := Parse([]byte(bad))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseRejectsCountryInTwoTiers(t *testing.T) {
	raw := `{
	  "version": 1, "default_tier": "a",
	  "tiers": {
	    "a": {"countries": ["US"], "currency": "USD", "trial_days": 3, "prices": {"p": 100}},
	    "b": {"countries": ["US"], "currency": "USD", "trial_days": 3, "prices": {"p": 200}}
	  },
	  "products": [{"product_id": "p", "title": "P", "period": "month", "period_days": 30}],
	  "experiment": {"name": "e", "split": [{"variant": "v", "weight": 100}]},
	  "variants": {"v": {"title": "T", "cta_template": "go", "highlight": "p"}}
	}`
	if _, err := Parse([]byte(raw)); err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("expected duplicate country error, got %v", err)
	}
}
