package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubPriceResolver struct {
	match ItemMatch
	err   error
}

func (s stubPriceResolver) Resolve(context.Context, string) (ItemMatch, error) {
	return s.match, s.err
}

type stubRegionalMarketFetcher struct {
	mu         sync.Mutex
	calls      []string
	failRegion string
	failAll    bool
}

func (s *stubRegionalMarketFetcher) RegionalMarket(_ context.Context, region string, _ int, historyEntries int) (RegionalMarketData, error) {
	s.mu.Lock()
	s.calls = append(s.calls, region)
	s.mu.Unlock()
	if s.failAll || region == s.failRegion {
		return RegionalMarketData{}, errors.New("upstream unavailable")
	}
	return RegionalMarketData{
		Region: region,
		Listing: &MarketListing{
			LastReviewTime: 1784473323,
			PricePerUnit:   12160,
			Quantity:       1,
			WorldName:      "Moogle",
		},
		RecentSales: []MarketSale{{
			PricePerUnit: 69999,
			Quantity:     historyEntries,
			Timestamp:    1784505390,
			WorldName:    "Twintania",
		}},
		RegularSaleVelocity: 5.14,
	}, nil
}

func TestPriceServiceLookupFetchesEveryRegionAndKeepsPartialResults(t *testing.T) {
	fetcher := &stubRegionalMarketFetcher{failRegion: "Japan"}
	service := &PriceService{
		resolver: stubPriceResolver{match: ItemMatch{
			Item:  CatalogItem{ID: 35573, Name: "Antique Wall Shelf"},
			Score: 1,
			Exact: true,
		}},
		market:         fetcher,
		historyEntries: 7,
	}
	lookup, err := service.Lookup(context.Background(), "antique wall shelf")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(lookup.Regions) != 4 {
		t.Fatalf("regions = %d, want 4", len(lookup.Regions))
	}
	if lookup.Regions[1].Region != "Japan" || lookup.Regions[1].Error == "" {
		t.Fatalf("Japan result = %#v, want partial error", lookup.Regions[1])
	}
	if lookup.Regions[2].Region != "North America" || lookup.Regions[2].Listing == nil {
		t.Fatalf("North America result = %#v", lookup.Regions[2])
	}
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	if len(fetcher.calls) != 4 {
		t.Fatalf("calls = %v", fetcher.calls)
	}
}

func TestPriceServiceLookupFailsWhenEveryRegionFails(t *testing.T) {
	service := &PriceService{
		resolver: stubPriceResolver{match: ItemMatch{Item: CatalogItem{ID: 35573, Name: "Antique Wall Shelf"}}},
		market:   &stubRegionalMarketFetcher{failAll: true},
	}
	if _, err := service.Lookup(context.Background(), "antique wall shelf"); err == nil {
		t.Fatal("Lookup error = nil, want failure")
	}
}

func TestPriceLookupFormatsDiscordBodyAndAnalysisData(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	lookup := PriceLookup{
		Query: "dancing wings",
		Match: ItemMatch{
			Item:  CatalogItem{ID: 27951, Name: "Dancing Wing"},
			Score: 0.92,
		},
		RetrievedAt: now,
		Regions: []RegionalMarketData{{
			Region: "Europe",
			Listing: &MarketListing{
				LastReviewTime: now.Add(-3 * time.Hour).Unix(),
				PricePerUnit:   1234567,
				Quantity:       2,
				WorldName:      "Moogle",
				HQ:             true,
			},
			RecentSales: []MarketSale{{
				PricePerUnit: 1400000,
				Quantity:     1,
				Timestamp:    now.Add(-6 * time.Hour).Unix(),
				WorldName:    "Odin",
			}},
		}},
	}
	templateData := lookup.TemplateData(now)
	if templateData.MatchedQuery != "dancing wings" {
		t.Fatalf("MatchedQuery = %q", templateData.MatchedQuery)
	}
	if templateData.Regions[0].Price != "1,234,567" || templateData.Regions[0].Quality != "HQ" || templateData.Regions[0].Age != "3 hours ago" {
		t.Fatalf("region template data = %#v", templateData.Regions[0])
	}

	analysis := lookup.AnalysisData()
	if analysis.ItemID != 27951 || len(analysis.Regions[0].RecentSales) != 1 {
		t.Fatalf("analysis = %#v", analysis)
	}
	if analysis.Regions[0].CurrentListing.PricePerUnit != 1234567 || analysis.Regions[0].RecentSales[0].PricePerUnit != 1400000 {
		t.Fatalf("analysis prices = %#v", analysis.Regions[0])
	}
}

func TestPriceHistoryIsIncludedInLlamaPrompt(t *testing.T) {
	var got llamaChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"The current listing is below recent completed sales, which may favor buying if the data is still fresh."}}]}`))
	}))
	defer server.Close()

	llama := &LlamaClient{
		url:         server.URL,
		maxTokens:   128,
		temperature: 0.2,
		httpClient:  server.Client(),
	}
	data := PriceAnalysisData{
		Query:    "antique wall shelf",
		ItemID:   35573,
		ItemName: "Antique Wall Shelf",
		Regions: []PriceAnalysisRegionData{{
			Region: "Europe",
			CurrentListing: &PriceAnalysisListingData{
				PricePerUnit: 12160,
				Quality:      "NQ",
				World:        "Moogle",
			},
			RecentSales: []PriceAnalysisSaleData{{
				PricePerUnit: 69999,
				Quantity:     1,
				Quality:      "NQ",
				World:        "Twintania",
			}},
		}},
	}
	note := composeLlamaNote(context.Background(), llama, LlamaNoteRequest{
		Kind:         "price",
		Body:         "Antique Wall Shelf prices",
		Data:         data,
		Instructions: priceNoteInstructions,
	})
	if !strings.Contains(note, "below recent completed sales") {
		t.Fatalf("note = %q", note)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages = %d", len(got.Messages))
	}
	prompt := got.Messages[0].Content
	for _, want := range []string{
		`"recent_sales"`,
		`"price_per_unit": 69999`,
		`"current_listing"`,
		"Distinguish listings from completed sales",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPriceResultTemplateRendersRegionalStates(t *testing.T) {
	messages, err := loadMessageTemplates("templates")
	if err != nil {
		t.Fatalf("loadMessageTemplates: %v", err)
	}
	body, err := messages.Render(templatePriceResult, PriceTemplateData{
		ItemName:       "Antique Wall Shelf",
		UniversalisURL: "https://universalis.app/market/35573",
		MatchedQuery:   "antique shelf",
		Regions: []PriceRegionTemplateData{
			{Name: "Oceania", HasListing: true, Price: "39,998", Quality: "NQ", World: "Sophia", Age: "3 days ago"},
			{Name: "Japan"},
			{Name: "Europe", Error: true},
		},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"Antique Wall Shelf",
		"Matched `antique shelf`",
		"39,998 Gil [NQ]",
		"Sophia",
		"No current listings found.",
		"Price data unavailable.",
		"Data via Universalis",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}
