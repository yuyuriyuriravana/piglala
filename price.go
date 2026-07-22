package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultPriceHistoryEntries = 20

var priceRegions = []struct {
	APIName     string
	DisplayName string
}{
	{APIName: "Oceania", DisplayName: "Oceania"},
	{APIName: "Japan", DisplayName: "Japan"},
	{APIName: "North-America", DisplayName: "North America"},
	{APIName: "Europe", DisplayName: "Europe"},
}

type priceItemResolver interface {
	Resolve(context.Context, string) (ItemMatch, error)
}

type regionalMarketFetcher interface {
	RegionalMarket(context.Context, string, int, int) (RegionalMarketData, error)
}

type PriceService struct {
	resolver       priceItemResolver
	market         regionalMarketFetcher
	historyEntries int
}

type PriceLookup struct {
	Query       string
	Match       ItemMatch
	Regions     []RegionalMarketData
	RetrievedAt time.Time
}

type PriceTemplateData struct {
	ItemName       string
	UniversalisURL string
	MatchedQuery   string
	Regions        []PriceRegionTemplateData
}

type PriceRegionTemplateData struct {
	Name       string
	HasListing bool
	Price      string
	Quality    string
	World      string
	Age        string
	Error      bool
}

type PriceQueryTemplateData struct {
	Query       string
	Suggestions []string
}

type PriceAnalysisData struct {
	Query       string                    `json:"query"`
	ItemID      int                       `json:"item_id"`
	ItemName    string                    `json:"item_name"`
	MatchScore  float64                   `json:"match_score"`
	RetrievedAt string                    `json:"retrieved_at"`
	Regions     []PriceAnalysisRegionData `json:"regions"`
}

type PriceAnalysisRegionData struct {
	Region              string                    `json:"region"`
	CurrentListing      *PriceAnalysisListingData `json:"current_listing,omitempty"`
	RecentSales         []PriceAnalysisSaleData   `json:"recent_sales"`
	RegularSaleVelocity float64                   `json:"sales_per_day"`
	DataError           string                    `json:"data_error,omitempty"`
}

type PriceAnalysisListingData struct {
	PricePerUnit int    `json:"price_per_unit"`
	Quantity     int    `json:"quantity"`
	Quality      string `json:"quality"`
	World        string `json:"world"`
	ReviewedAt   string `json:"reviewed_at"`
}

type PriceAnalysisSaleData struct {
	PricePerUnit int    `json:"price_per_unit"`
	Quantity     int    `json:"quantity"`
	Quality      string `json:"quality"`
	World        string `json:"world"`
	SoldAt       string `json:"sold_at"`
}

func newPriceServiceFromEnv(store *Store) *PriceService {
	universalis := newUniversalisClientFromEnv()
	return &PriceService{
		resolver:       newItemCatalogFromEnv(store, universalis),
		market:         universalis,
		historyEntries: getenvPositiveInt("PRICE_HISTORY_ENTRIES", defaultPriceHistoryEntries),
	}
}

func (s *PriceService) Warm(ctx context.Context) error {
	catalog, ok := s.resolver.(*itemCatalog)
	if !ok {
		return nil
	}
	return catalog.Warm(ctx)
}

func (s *PriceService) Lookup(ctx context.Context, query string) (PriceLookup, error) {
	if s == nil || s.resolver == nil || s.market == nil {
		return PriceLookup{}, fmt.Errorf("price service is not configured")
	}
	match, err := s.resolver.Resolve(ctx, query)
	if err != nil {
		return PriceLookup{}, err
	}

	results := make([]RegionalMarketData, len(priceRegions))
	var wg sync.WaitGroup
	for i, region := range priceRegions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := s.market.RegionalMarket(ctx, region.APIName, match.Item.ID, s.historyEntries)
			data.Region = region.DisplayName
			if err != nil {
				data.Error = err.Error()
			}
			results[i] = data
		}()
	}
	wg.Wait()

	successes := 0
	var fetchErrors []error
	for _, result := range results {
		if result.Error == "" {
			successes++
		} else {
			fetchErrors = append(fetchErrors, errors.New(result.Error))
		}
	}
	if successes == 0 {
		return PriceLookup{}, fmt.Errorf("fetch regional prices: %w", errors.Join(fetchErrors...))
	}
	return PriceLookup{
		Query:       strings.TrimSpace(query),
		Match:       match,
		Regions:     results,
		RetrievedAt: time.Now().UTC(),
	}, nil
}

func (l PriceLookup) TemplateData(now time.Time) PriceTemplateData {
	data := PriceTemplateData{
		ItemName:       discordSafeText(l.Match.Item.Name),
		UniversalisURL: fmt.Sprintf("https://universalis.app/market/%d", l.Match.Item.ID),
	}
	if normalizeItemName(l.Query) != normalizeItemName(l.Match.Item.Name) {
		data.MatchedQuery = discordSafeText(l.Query)
	}
	for _, region := range l.Regions {
		row := PriceRegionTemplateData{
			Name:  region.Region,
			Error: region.Error != "",
		}
		if region.Listing != nil {
			row.HasListing = true
			row.Price = formatGil(region.Listing.PricePerUnit)
			row.Quality = itemQuality(region.Listing.HQ)
			row.World = discordSafeText(region.Listing.WorldName)
			if region.Listing.LastReviewTime > 0 {
				row.Age = formatRelativeTime(time.Unix(region.Listing.LastReviewTime, 0), now)
			} else {
				row.Age = "unknown"
			}
		}
		data.Regions = append(data.Regions, row)
	}
	return data
}

func (l PriceLookup) AnalysisData() PriceAnalysisData {
	data := PriceAnalysisData{
		Query:       l.Query,
		ItemID:      l.Match.Item.ID,
		ItemName:    l.Match.Item.Name,
		MatchScore:  l.Match.Score,
		RetrievedAt: l.RetrievedAt.Format(time.RFC3339),
	}
	for _, region := range l.Regions {
		regionData := PriceAnalysisRegionData{
			Region:              region.Region,
			RegularSaleVelocity: region.RegularSaleVelocity,
			DataError:           region.Error,
		}
		if region.Listing != nil {
			reviewedAt := ""
			if region.Listing.LastReviewTime > 0 {
				reviewedAt = time.Unix(region.Listing.LastReviewTime, 0).UTC().Format(time.RFC3339)
			}
			regionData.CurrentListing = &PriceAnalysisListingData{
				PricePerUnit: region.Listing.PricePerUnit,
				Quantity:     region.Listing.Quantity,
				Quality:      itemQuality(region.Listing.HQ),
				World:        region.Listing.WorldName,
				ReviewedAt:   reviewedAt,
			}
		}
		for _, sale := range region.RecentSales {
			soldAt := ""
			if sale.Timestamp > 0 {
				soldAt = time.Unix(sale.Timestamp, 0).UTC().Format(time.RFC3339)
			}
			regionData.RecentSales = append(regionData.RecentSales, PriceAnalysisSaleData{
				PricePerUnit: sale.PricePerUnit,
				Quantity:     sale.Quantity,
				Quality:      itemQuality(sale.HQ),
				World:        sale.WorldName,
				SoldAt:       soldAt,
			})
		}
		data.Regions = append(data.Regions, regionData)
	}
	return data
}

func priceResolutionTemplateData(err *ItemResolutionError) PriceQueryTemplateData {
	data := PriceQueryTemplateData{Query: discordSafeText(err.Query)}
	for _, suggestion := range err.Suggestions {
		data.Suggestions = append(data.Suggestions, discordSafeText(suggestion.Item.Name))
	}
	return data
}

func itemQuality(hq bool) string {
	if hq {
		return "HQ"
	}
	return "NQ"
}

func formatGil(value int) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	digits := strconv.Itoa(value)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return sign + digits
}

func formatRelativeTime(then, now time.Time) string {
	if then.IsZero() {
		return "unknown"
	}
	elapsed := now.Sub(then)
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		minutes := int(elapsed / time.Minute)
		return pluralTime(minutes, "minute")
	case elapsed < 24*time.Hour:
		hours := int(elapsed / time.Hour)
		return pluralTime(hours, "hour")
	case elapsed < 14*24*time.Hour:
		days := int(elapsed / (24 * time.Hour))
		return pluralTime(days, "day")
	default:
		weeks := int(elapsed / (7 * 24 * time.Hour))
		return pluralTime(weeks, "week")
	}
}

func pluralTime(value int, unit string) string {
	if value != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%d %s ago", value, unit)
}
