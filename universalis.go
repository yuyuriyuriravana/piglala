package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultUniversalisBaseURL = "https://universalis.app/api/v2"

type universalisClient struct {
	baseURL    string
	httpClient *http.Client
}

type MarketListing struct {
	LastReviewTime int64  `json:"lastReviewTime"`
	PricePerUnit   int    `json:"pricePerUnit"`
	Quantity       int    `json:"quantity"`
	WorldName      string `json:"worldName"`
	HQ             bool   `json:"hq"`
}

type MarketSale struct {
	PricePerUnit int    `json:"pricePerUnit"`
	Quantity     int    `json:"quantity"`
	Timestamp    int64  `json:"timestamp"`
	WorldName    string `json:"worldName"`
	HQ           bool   `json:"hq"`
}

type RegionalMarketData struct {
	Region              string         `json:"region"`
	LastUploadTime      int64          `json:"last_upload_time_ms"`
	Listing             *MarketListing `json:"current_listing,omitempty"`
	RecentSales         []MarketSale   `json:"recent_sales"`
	RegularSaleVelocity float64        `json:"sales_per_day"`
	Error               string         `json:"error,omitempty"`
}

func newUniversalisClientFromEnv() *universalisClient {
	return &universalisClient{
		baseURL: strings.TrimRight(getenvDefault("UNIVERSALIS_BASE_URL", defaultUniversalisBaseURL), "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *universalisClient) MarketableItemIDs(ctx context.Context) (map[int]struct{}, error) {
	body, err := c.get(ctx, c.baseURL+"/marketable")
	if err != nil {
		return nil, fmt.Errorf("fetch marketable item IDs: %w", err)
	}
	var ids []int
	if err := json.Unmarshal(body, &ids); err != nil {
		return nil, fmt.Errorf("decode marketable item IDs: %w", err)
	}
	out := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Universalis returned no marketable item IDs")
	}
	return out, nil
}

func (c *universalisClient) RegionalMarket(ctx context.Context, region string, itemID, historyEntries int) (RegionalMarketData, error) {
	endpoint, err := url.Parse(c.baseURL + "/" + url.PathEscape(region) + "/" + strconv.Itoa(itemID))
	if err != nil {
		return RegionalMarketData{}, fmt.Errorf("build Universalis URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("listings", "1")
	query.Set("entries", strconv.Itoa(historyEntries))
	endpoint.RawQuery = query.Encode()

	body, err := c.get(ctx, endpoint.String())
	if err != nil {
		return RegionalMarketData{}, fmt.Errorf("fetch %s market data: %w", region, err)
	}
	var response struct {
		LastUploadTime      int64           `json:"lastUploadTime"`
		Listings            []MarketListing `json:"listings"`
		RecentHistory       []MarketSale    `json:"recentHistory"`
		RegularSaleVelocity float64         `json:"regularSaleVelocity"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return RegionalMarketData{}, fmt.Errorf("decode %s market data: %w", region, err)
	}
	result := RegionalMarketData{
		Region:              region,
		LastUploadTime:      response.LastUploadTime,
		RecentSales:         response.RecentHistory,
		RegularSaleVelocity: response.RegularSaleVelocity,
	}
	if len(response.Listings) > 0 {
		listing := response.Listings[0]
		result.Listing = &listing
	}
	return result, nil
}

func (c *universalisClient) get(ctx context.Context, endpoint string) ([]byte, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("Universalis client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "piglala")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Universalis returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}
