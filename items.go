package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	defaultXIVAPIBaseURL      = "https://v2.xivapi.com/api"
	defaultCatalogRefresh     = 7 * 24 * time.Hour
	catalogRetryDelay         = 10 * time.Minute
	xivAPIItemPageSize        = 1000
	itemCatalogRefreshMetaKey = "refreshed_at"
)

type CatalogItem struct {
	ID   int    `json:"item_id"`
	Name string `json:"name"`
}

type ItemMatch struct {
	Item  CatalogItem `json:"item"`
	Score float64     `json:"score"`
	Exact bool        `json:"exact"`
}

type ItemResolutionError struct {
	Query       string
	Suggestions []ItemMatch
}

func (e *ItemResolutionError) Error() string {
	if len(e.Suggestions) > 0 {
		return fmt.Sprintf("item query %q is ambiguous", e.Query)
	}
	return fmt.Sprintf("no marketable item matches %q", e.Query)
}

type xivAPIClient struct {
	baseURL    string
	httpClient *http.Client
}

type itemCatalog struct {
	store           *Store
	xivapi          *xivAPIClient
	universalis     *universalisClient
	refreshInterval time.Duration

	mu          sync.Mutex
	loaded      bool
	items       []CatalogItem
	refreshedAt time.Time
	lastAttempt time.Time
}

func newItemCatalogFromEnv(store *Store, universalis *universalisClient) *itemCatalog {
	refreshHours := getenvPositiveInt("ITEM_CATALOG_REFRESH_HOURS", int(defaultCatalogRefresh.Hours()))
	return &itemCatalog{
		store: store,
		xivapi: &xivAPIClient{
			baseURL: strings.TrimRight(getenvDefault("XIVAPI_BASE_URL", defaultXIVAPIBaseURL), "/"),
			httpClient: &http.Client{
				Timeout: 30 * time.Second,
			},
		},
		universalis:     universalis,
		refreshInterval: time.Duration(refreshHours) * time.Hour,
	}
}

func (c *itemCatalog) Warm(ctx context.Context) error {
	_, err := c.ensure(ctx)
	return err
}

func (c *itemCatalog) Resolve(ctx context.Context, query string) (ItemMatch, error) {
	items, err := c.ensure(ctx)
	if err != nil {
		return ItemMatch{}, err
	}
	return resolveCatalogItem(items, query)
}

func (c *itemCatalog) ensure(ctx context.Context) ([]CatalogItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loaded {
		items, refreshedAt, err := c.store.LoadItemCatalog()
		if err != nil {
			return nil, err
		}
		c.items = items
		c.refreshedAt = refreshedAt
		c.loaded = true
	}

	now := time.Now()
	fresh := len(c.items) > 0 && !c.refreshedAt.IsZero() && now.Sub(c.refreshedAt) < c.refreshInterval
	if fresh {
		return append([]CatalogItem(nil), c.items...), nil
	}
	if len(c.items) > 0 && !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < catalogRetryDelay {
		return append([]CatalogItem(nil), c.items...), nil
	}

	c.lastAttempt = now
	items, err := c.fetch(ctx)
	if err != nil {
		if len(c.items) > 0 {
			log.Printf("items: refresh failed; using stale catalog: %v", err)
			return append([]CatalogItem(nil), c.items...), nil
		}
		return nil, fmt.Errorf("refresh item catalog: %w", err)
	}
	refreshedAt := time.Now().UTC()
	if err := c.store.ReplaceItemCatalog(items, refreshedAt); err != nil {
		return nil, err
	}
	c.items = items
	c.refreshedAt = refreshedAt
	log.Printf("items: refreshed %d marketable item(s)", len(items))
	return append([]CatalogItem(nil), items...), nil
}

func (c *itemCatalog) fetch(ctx context.Context) ([]CatalogItem, error) {
	marketable, err := c.universalis.MarketableItemIDs(ctx)
	if err != nil {
		return nil, err
	}
	items, err := c.xivapi.AllItemNames(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]CatalogItem, 0, len(marketable))
	for _, item := range items {
		if _, ok := marketable[item.ID]; ok && strings.TrimSpace(item.Name) != "" {
			filtered = append(filtered, item)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})
	if len(filtered) == 0 {
		return nil, fmt.Errorf("catalog contained no named marketable items")
	}
	return filtered, nil
}

func (c *xivAPIClient) AllItemNames(ctx context.Context) ([]CatalogItem, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("xivapi client is not configured")
	}

	var items []CatalogItem
	after := -1
	for page := 0; page < 1000; page++ {
		endpoint, err := url.Parse(c.baseURL + "/sheet/Item")
		if err != nil {
			return nil, fmt.Errorf("build xivapi item URL: %w", err)
		}
		query := endpoint.Query()
		query.Set("fields", "Name")
		query.Set("language", "en")
		query.Set("limit", strconv.Itoa(xivAPIItemPageSize))
		if after >= 0 {
			query.Set("after", strconv.Itoa(after))
		}
		endpoint.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("build xivapi item request: %w", err)
		}
		req.Header.Set("User-Agent", "piglala")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch xivapi item page: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read xivapi item page: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("xivapi returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}

		var pageData struct {
			Rows []struct {
				RowID  int `json:"row_id"`
				Fields struct {
					Name string `json:"Name"`
				} `json:"fields"`
			} `json:"rows"`
		}
		if err := json.Unmarshal(body, &pageData); err != nil {
			return nil, fmt.Errorf("decode xivapi item page: %w", err)
		}
		if len(pageData.Rows) == 0 {
			return items, nil
		}
		for _, row := range pageData.Rows {
			items = append(items, CatalogItem{ID: row.RowID, Name: row.Fields.Name})
		}
		nextAfter := pageData.Rows[len(pageData.Rows)-1].RowID
		if nextAfter <= after {
			return nil, fmt.Errorf("xivapi item pagination did not advance past %d", after)
		}
		after = nextAfter
	}
	return nil, fmt.Errorf("xivapi item pagination exceeded safety limit")
}

func (s *Store) LoadItemCatalog() ([]CatalogItem, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT item_id, name FROM item_catalog ORDER BY item_id`)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load item catalog: %w", err)
	}
	defer rows.Close()

	var items []CatalogItem
	for rows.Next() {
		var item CatalogItem
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan item catalog: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("iterate item catalog: %w", err)
	}

	var refreshedValue string
	err = s.db.QueryRow(`
SELECT value
FROM item_catalog_metadata
WHERE key = ?`, itemCatalogRefreshMetaKey).Scan(&refreshedValue)
	if err != nil && err != sql.ErrNoRows {
		return nil, time.Time{}, fmt.Errorf("load item catalog refresh time: %w", err)
	}
	if err == sql.ErrNoRows {
		return items, time.Time{}, nil
	}
	refreshedAt, err := time.Parse(time.RFC3339Nano, refreshedValue)
	if err != nil {
		return items, time.Time{}, nil
	}
	return items, refreshedAt, nil
}

func (s *Store) ReplaceItemCatalog(items []CatalogItem, refreshedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin item catalog refresh: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM item_catalog`); err != nil {
		return fmt.Errorf("clear item catalog: %w", err)
	}
	stmt, err := tx.Prepare(`
INSERT INTO item_catalog (item_id, name, normalized_name, updated_at)
VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare item catalog insert: %w", err)
	}
	defer stmt.Close()

	updatedAt := refreshedAt.UTC().Format(time.RFC3339Nano)
	for _, item := range items {
		normalized := normalizeItemName(item.Name)
		if item.ID <= 0 || normalized == "" {
			continue
		}
		if _, err := stmt.Exec(item.ID, strings.TrimSpace(item.Name), normalized, updatedAt); err != nil {
			return fmt.Errorf("insert item catalog item %d: %w", item.ID, err)
		}
	}
	if _, err := tx.Exec(`
INSERT INTO item_catalog_metadata (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		itemCatalogRefreshMetaKey, refreshedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("save item catalog refresh time: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit item catalog refresh: %w", err)
	}
	return nil
}

func resolveCatalogItem(items []CatalogItem, query string) (ItemMatch, error) {
	normalizedQuery := normalizeItemName(query)
	if normalizedQuery == "" {
		return ItemMatch{}, &ItemResolutionError{Query: strings.TrimSpace(query)}
	}

	matches := make([]ItemMatch, 0, len(items))
	for _, item := range items {
		normalizedName := normalizeItemName(item.Name)
		if normalizedName == "" {
			continue
		}
		score := itemNameSimilarity(normalizedQuery, normalizedName)
		if score >= 0.60 {
			matches = append(matches, ItemMatch{
				Item:  item,
				Score: score,
				Exact: normalizedQuery == normalizedName,
			})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].Item.Name < matches[j].Item.Name
		}
		return matches[i].Score > matches[j].Score
	})
	if len(matches) == 0 {
		return ItemMatch{}, &ItemResolutionError{Query: strings.TrimSpace(query)}
	}
	if matches[0].Exact {
		return matches[0], nil
	}

	const (
		autoAcceptScore  = 0.88
		autoAcceptMargin = 0.04
	)
	margin := 1.0
	if len(matches) > 1 {
		margin = matches[0].Score - matches[1].Score
	}
	if matches[0].Score >= autoAcceptScore && margin >= autoAcceptMargin {
		return matches[0], nil
	}

	limit := 3
	if len(matches) < limit {
		limit = len(matches)
	}
	return ItemMatch{}, &ItemResolutionError{
		Query:       strings.TrimSpace(query),
		Suggestions: append([]ItemMatch(nil), matches[:limit]...),
	}
}

func normalizeItemName(value string) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			pendingSpace = false
		} else {
			pendingSpace = true
		}
	}
	return b.String()
}

func itemNameSimilarity(query, candidate string) float64 {
	if query == candidate {
		return 1
	}
	score := levenshteinSimilarity(query, candidate)
	queryTokens := strings.Fields(query)
	candidateTokens := strings.Fields(candidate)
	sort.Strings(queryTokens)
	sort.Strings(candidateTokens)
	score = math.Max(score, levenshteinSimilarity(strings.Join(queryTokens, " "), strings.Join(candidateTokens, " ")))

	shorter, longer := query, candidate
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if strings.Contains(longer, shorter) {
		coverage := float64(len(shorter)) / float64(len(longer))
		score = math.Max(score, 0.84+0.14*coverage)
	}
	return math.Min(score, 1)
}

func levenshteinSimilarity(a, b string) float64 {
	ar := []rune(a)
	br := []rune(b)
	maxLen := len(ar)
	if len(br) > maxLen {
		maxLen = len(br)
	}
	if maxLen == 0 {
		return 1
	}
	return 1 - float64(levenshteinDistance(ar, br))/float64(maxLen)
}

func levenshteinDistance(a, b []rune) int {
	if len(a) > len(b) {
		a, b = b, a
	}
	previous := make([]int, len(a)+1)
	current := make([]int, len(a)+1)
	for i := range previous {
		previous[i] = i
	}
	for row, rb := range b {
		current[0] = row + 1
		for col, ra := range a {
			cost := 0
			if ra != rb {
				cost = 1
			}
			current[col+1] = min(
				current[col]+1,
				previous[col+1]+1,
				previous[col]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(a)]
}
