package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/androidpublisher/v3"

	"github.com/allen-hsu/gpc/internal/play"
)

// ---- in-app products (one-time / managed) ----

var iapCmd = &cobra.Command{
	Use:   "iap",
	Short: "One-time in-app products (monetization.onetimeproducts)",
	Long: `Console page: Monetize → Products → In-app products.

Uses the new monetization.onetimeproducts resource. The legacy inappproducts
resource answers 403 "Please migrate to the new publishing API" for apps
created recently, so gpc does not use it. A product = productId + purchase
options (buy/rent) with regional prices; RevenueCat maps "productId" (and
"productId:purchaseOptionId" for multi-option products).

Read-only for now: the Console form is still the safer place for tax settings.`,
}

var iapListCmd = &cobra.Command{
	Use:   "list",
	Short: "List one-time products with their purchase options",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		all := []*androidpublisher.OneTimeProduct{}
		token := ""
		for {
			call := c.Svc.Monetization.Onetimeproducts.List(c.Package)
			if token != "" {
				call = call.PageToken(token)
			}
			res, err := call.Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("list one-time products", err)
			}
			all = append(all, res.OneTimeProducts...)
			if res.NextPageToken == "" {
				break
			}
			token = res.NextPageToken
		}
		rows := [][]string{}
		for _, p := range all {
			title := ""
			for _, l := range p.Listings {
				if title == "" || l.LanguageCode == "en-US" {
					title = l.Title
				}
			}
			if len(p.PurchaseOptions) == 0 {
				rows = append(rows, []string{p.ProductId, "-", "-", "-", play.Truncate(title, 30)})
			}
			for _, po := range p.PurchaseOptions {
				kind := "buy"
				if po.RentOption != nil {
					kind = "rent"
				}
				rows = append(rows, []string{p.ProductId, po.PurchaseOptionId, po.State, kind, play.Truncate(title, 30)})
			}
		}
		return emit(all, []string{"PRODUCT", "PURCHASE OPTION", "STATE", "KIND", "TITLE"}, rows)
	},
}

// ---- subscriptions (base plans + offers) ----

var subsCmd = &cobra.Command{
	Use:   "subscriptions",
	Short: "Subscriptions, base plans and offers (monetization.subscriptions)",
	Long: `Console page: Monetize → Products → Subscriptions.

A subscription = productId + ≥1 base plan (billing period, renewal type,
regional prices) + optional offers (free trial, intro price). RevenueCat maps
its products to "productId:basePlanId" — this list gives you both halves.`,
}

var subsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List subscriptions with their base plans",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		all := []*androidpublisher.Subscription{}
		token := ""
		for {
			call := c.Svc.Monetization.Subscriptions.List(c.Package)
			if token != "" {
				call = call.PageToken(token)
			}
			res, err := call.Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("list subscriptions", err)
			}
			all = append(all, res.Subscriptions...)
			if res.NextPageToken == "" {
				break
			}
			token = res.NextPageToken
		}
		rows := [][]string{}
		for _, s := range all {
			title := ""
			for _, l := range s.Listings {
				if title == "" || l.LanguageCode == "en-US" {
					title = l.Title
				}
			}
			if len(s.BasePlans) == 0 {
				rows = append(rows, []string{s.ProductId, "-", "-", "-", "-", play.Truncate(title, 30)})
			}
			for _, bp := range s.BasePlans {
				period, renew := "-", "prepaid"
				if bp.AutoRenewingBasePlanType != nil {
					period, renew = bp.AutoRenewingBasePlanType.BillingPeriodDuration, "autoRenewing"
				} else if bp.PrepaidBasePlanType != nil {
					period = bp.PrepaidBasePlanType.BillingPeriodDuration
				}
				rows = append(rows, []string{s.ProductId, bp.BasePlanId, bp.State, renew, period, play.Truncate(title, 30)})
			}
		}
		return emit(all, []string{"PRODUCT", "BASE PLAN", "STATE", "RENEWAL", "PERIOD", "TITLE"}, rows)
	},
}

// ---- regional price conversion ----

var (
	priceCurrency string
)

var pricingCmd = &cobra.Command{
	Use:   "pricing",
	Short: "Regional price helpers",
}

var pricingConvertCmd = &cobra.Command{
	Use:   "convert <amount>",
	Short: "Convert a price into every Play region using Google's current rates and rounding",
	Long: `Pure computation, nothing is written. Mirrors the "Set prices" dialog in
Console — use it to preview what a USD/TWD base price becomes per region and
to feed RevenueCat or a PPP spreadsheet.

  gpc pricing convert 4.99 --currency USD
  gpc pricing convert 150 --currency TWD | jq '.JP'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		units, nanos, err := parseMoney(args[0])
		if err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		req := &androidpublisher.ConvertRegionPricesRequest{Price: &androidpublisher.Money{CurrencyCode: strings.ToUpper(priceCurrency), Units: units, Nanos: nanos}}
		res, err := c.Svc.Monetization.ConvertRegionPrices(c.Package, req).Context(cmd.Context()).Do()
		if err != nil {
			return play.Wrap("convert region prices", err)
		}
		regions := make([]string, 0, len(res.ConvertedRegionPrices))
		for r := range res.ConvertedRegionPrices {
			regions = append(regions, r)
		}
		sort.Strings(regions)
		rows := make([][]string, 0, len(regions))
		for _, r := range regions {
			p := res.ConvertedRegionPrices[r]
			price, tax := "", ""
			if p.Price != nil {
				price = money(p.Price)
			}
			if p.TaxAmount != nil {
				tax = money(p.TaxAmount)
			}
			rows = append(rows, []string{r, price, tax})
		}
		return emit(res.ConvertedRegionPrices, []string{"REGION", "PRICE", "TAX"}, rows)
	},
}

func parseMoney(s string) (units int64, nanos int64, err error) {
	parts := strings.SplitN(s, ".", 2)
	units, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad amount %q", s)
	}
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) > 9 {
			frac = frac[:9]
		}
		frac += strings.Repeat("0", 9-len(frac))
		nanos, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("bad amount %q", s)
		}
	}
	return units, nanos, nil
}

func money(m *androidpublisher.Money) string {
	return fmt.Sprintf("%s %s", strconv.FormatFloat(float64(m.Units)+float64(m.Nanos)/1e9, 'f', 2, 64), m.CurrencyCode)
}

func init() {
	iapCmd.AddCommand(iapListCmd)
	subsCmd.AddCommand(subsListCmd)
	pricingConvertCmd.Flags().StringVar(&priceCurrency, "currency", "USD", "ISO 4217 currency of <amount>")
	pricingCmd.AddCommand(pricingConvertCmd)
	rootCmd.AddCommand(iapCmd, subsCmd, pricingCmd)
}
