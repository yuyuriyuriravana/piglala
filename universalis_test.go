package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUniversalisRegionalMarketIncludesListingAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Europe/35573" {
			t.Fatalf("path = %q, want /Europe/35573", r.URL.Path)
		}
		if got := r.URL.Query().Get("listings"); got != "1" {
			t.Fatalf("listings = %q", got)
		}
		if got := r.URL.Query().Get("entries"); got != "7" {
			t.Fatalf("entries = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"lastUploadTime": 1784561837089,
			"listings": [{
				"lastReviewTime": 1784473323,
				"pricePerUnit": 12160,
				"quantity": 1,
				"worldName": "Moogle",
				"hq": false
			}],
			"recentHistory": [{
				"pricePerUnit": 69999,
				"quantity": 1,
				"timestamp": 1784505390,
				"worldName": "Twintania",
				"hq": false
			}],
			"regularSaleVelocity": 5.14
		}`))
	}))
	defer server.Close()

	client := &universalisClient{baseURL: server.URL, httpClient: server.Client()}
	data, err := client.RegionalMarket(context.Background(), "Europe", 35573, 7)
	if err != nil {
		t.Fatalf("RegionalMarket: %v", err)
	}
	if data.Listing == nil || data.Listing.PricePerUnit != 12160 || data.Listing.WorldName != "Moogle" {
		t.Fatalf("listing = %#v", data.Listing)
	}
	if len(data.RecentSales) != 1 || data.RecentSales[0].PricePerUnit != 69999 {
		t.Fatalf("recent sales = %#v", data.RecentSales)
	}
	if data.RegularSaleVelocity != 5.14 {
		t.Fatalf("sale velocity = %v", data.RegularSaleVelocity)
	}
}

func TestUniversalisMarketableItemIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[27951,35573]`))
	}))
	defer server.Close()

	client := &universalisClient{baseURL: server.URL, httpClient: server.Client()}
	ids, err := client.MarketableItemIDs(context.Background())
	if err != nil {
		t.Fatalf("MarketableItemIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %#v", ids)
	}
	if _, ok := ids[35573]; !ok {
		t.Fatal("35573 is missing")
	}
}
