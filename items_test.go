package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveCatalogItemExactAndFuzzy(t *testing.T) {
	items := []CatalogItem{
		{ID: 27951, Name: "Dancing Wing"},
		{ID: 30001, Name: "Dancing Shoes"},
		{ID: 35573, Name: "Antique Wall Shelf"},
	}

	exact, err := resolveCatalogItem(items, "  ANTIQUE wall-shelf ")
	if err != nil {
		t.Fatalf("exact resolve: %v", err)
	}
	if exact.Item.ID != 35573 || !exact.Exact {
		t.Fatalf("exact match = %#v, want Antique Wall Shelf exact", exact)
	}

	fuzzy, err := resolveCatalogItem(items, "dancing wings")
	if err != nil {
		t.Fatalf("fuzzy resolve: %v", err)
	}
	if fuzzy.Item.ID != 27951 || fuzzy.Exact || fuzzy.Score < 0.88 {
		t.Fatalf("fuzzy match = %#v, want Dancing Wing with high confidence", fuzzy)
	}
}

func TestResolveCatalogItemReturnsSuggestionsForAmbiguousQuery(t *testing.T) {
	items := []CatalogItem{
		{ID: 1, Name: "Antique Wall Shelf"},
		{ID: 2, Name: "Mounted Wall Shelf"},
		{ID: 3, Name: "Wall Shelf"},
	}
	_, err := resolveCatalogItem(items, "wall shelves")
	var resolutionErr *ItemResolutionError
	if !errors.As(err, &resolutionErr) {
		t.Fatalf("error = %v, want ItemResolutionError", err)
	}
	if len(resolutionErr.Suggestions) == 0 {
		t.Fatal("suggestions are empty")
	}
}

func TestItemCatalogRefreshesAndPersistsMarketableItems(t *testing.T) {
	xivServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		w.Header().Set("Content-Type", "application/json")
		switch after {
		case "":
			w.Write([]byte(`{"rows":[
				{"row_id":27951,"fields":{"Name":"Dancing Wing"}},
				{"row_id":30000,"fields":{"Name":"Unmarketable Test Item"}}
			]}`))
		case "30000":
			w.Write([]byte(`{"rows":[{"row_id":35573,"fields":{"Name":"Antique Wall Shelf"}}]}`))
		case "35573":
			w.Write([]byte(`{"rows":[]}`))
		default:
			t.Fatalf("unexpected after query %q", after)
		}
	}))
	defer xivServer.Close()

	universalisServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/marketable" {
			t.Fatalf("path = %q, want /marketable", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[27951,35573]`))
	}))
	defer universalisServer.Close()

	store := newTestStore(t)
	catalog := &itemCatalog{
		store: store,
		xivapi: &xivAPIClient{
			baseURL:    xivServer.URL,
			httpClient: xivServer.Client(),
		},
		universalis: &universalisClient{
			baseURL:    universalisServer.URL,
			httpClient: universalisServer.Client(),
		},
		refreshInterval: time.Hour,
	}
	if err := catalog.Warm(context.Background()); err != nil {
		t.Fatalf("Warm: %v", err)
	}

	items, refreshedAt, err := store.LoadItemCatalog()
	if err != nil {
		t.Fatalf("LoadItemCatalog: %v", err)
	}
	if len(items) != 2 || items[0].ID != 27951 || items[1].ID != 35573 {
		t.Fatalf("items = %#v, want only two marketable items", items)
	}
	if refreshedAt.IsZero() {
		t.Fatal("refreshedAt is zero")
	}
	match, err := catalog.Resolve(context.Background(), "dancing wings")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if match.Item.ID != 27951 {
		t.Fatalf("match = %#v, want Dancing Wing", match)
	}

	var normalized string
	if err := store.db.QueryRow(`SELECT normalized_name FROM item_catalog WHERE item_id = 35573`).Scan(&normalized); err != nil {
		t.Fatalf("query normalized name: %v", err)
	}
	if normalized != "antique wall shelf" {
		t.Fatalf("normalized = %q", normalized)
	}
}

func TestXIVAPIItemPaginationEncodesParameters(t *testing.T) {
	var requests []*url.URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		copyURL := *r.URL
		requests = append(requests, &copyURL)
		if r.URL.Query().Get("after") == "" {
			w.Write([]byte(`{"rows":[{"row_id":42,"fields":{"Name":"Test Item"}}]}`))
			return
		}
		w.Write([]byte(`{"rows":[]}`))
	}))
	defer server.Close()

	client := &xivAPIClient{baseURL: server.URL, httpClient: server.Client()}
	items, err := client.AllItemNames(context.Background())
	if err != nil {
		t.Fatalf("AllItemNames: %v", err)
	}
	if len(items) != 1 || items[0].ID != 42 {
		t.Fatalf("items = %#v", items)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if got := requests[0].Query().Get("fields"); got != "Name" {
		t.Fatalf("fields = %q", got)
	}
	if got := requests[0].Query().Get("language"); got != "en" {
		t.Fatalf("language = %q", got)
	}
	if got := requests[1].Query().Get("after"); got != "42" {
		t.Fatalf("after = %q", got)
	}
	if !strings.HasSuffix(requests[0].Path, "/sheet/Item") {
		t.Fatalf("path = %q", requests[0].Path)
	}
}

func TestItemCatalogPersistsAcrossStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite3")
	store, err := loadStore(path)
	if err != nil {
		t.Fatalf("loadStore: %v", err)
	}
	refreshedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if err := store.ReplaceItemCatalog([]CatalogItem{{ID: 35573, Name: "Antique Wall Shelf"}}, refreshedAt); err != nil {
		t.Fatalf("ReplaceItemCatalog: %v", err)
	}
	store.Close()

	store, err = loadStore(path)
	if err != nil {
		t.Fatalf("reopen loadStore: %v", err)
	}
	defer store.Close()
	items, gotRefresh, err := store.LoadItemCatalog()
	if err != nil {
		t.Fatalf("LoadItemCatalog: %v", err)
	}
	if len(items) != 1 || items[0].ID != 35573 || !gotRefresh.Equal(refreshedAt) {
		t.Fatalf("items/refreshed = %#v %v", items, gotRefresh)
	}
}
