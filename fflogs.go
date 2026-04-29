package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	fflogsTokenURL = "https://www.fflogs.com/oauth/token"
	fflogsAPIURL   = "https://www.fflogs.com/api/v2/client"
)

type fflogsClient struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	token        string
	tokenExpiry  time.Time
}

func newFFLogsClient(clientID, clientSecret string) *fflogsClient {
	return &fflogsClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *fflogsClient) ensureToken() error {
	if !c.tokenExpiry.IsZero() && time.Now().Add(60*time.Second).Before(c.tokenExpiry) {
		return nil
	}

	log.Println("fflogs: refreshing OAuth token")
	start := time.Now()
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", fflogsTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token fetch failed (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}

	c.token = result.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	log.Printf("fflogs: OAuth token refreshed in %v expires_in=%s", time.Since(start).Round(time.Millisecond), time.Until(c.tokenExpiry).Round(time.Second))
	return nil
}

func (c *fflogsClient) query(gql string, variables map[string]any) ([]byte, error) {
	if err := c.ensureToken(); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	payload, err := json.Marshal(map[string]any{"query": gql, "variables": variables})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fflogsAPIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed (%d): %s", resp.StatusCode, respBody)
	}
	log.Printf("fflogs: GraphQL query completed status=%d bytes=%d duration=%v", resp.StatusCode, len(respBody), time.Since(start).Round(time.Millisecond))
	return respBody, nil
}

// RelevantZone is a (zone, difficulty) pair matching Savage, Extreme, or Ultimate.
type RelevantZone struct {
	ZoneID         int
	ZoneName       string
	DifficultyID   int
	DifficultyName string
}

type CharacterRanking struct {
	EncounterID   int
	EncounterName string
	RankPercent   float64
}

const zonesQuery = `
query {
  worldData {
    zones {
      id
      name
      difficulties {
        id
        name
      }
    }
  }
}`

var relevantDifficultyNames = map[string]bool{
	"savage":   true,
	"extreme":  true,
	"ultimate": true,
}

func isRelevantDifficulty(name string) bool {
	return relevantDifficultyNames[strings.ToLower(strings.TrimSpace(name))]
}

func (c *fflogsClient) GetRelevantZones() ([]RelevantZone, error) {
	data, err := c.query(zonesQuery, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			WorldData struct {
				Zones []struct {
					ID           int    `json:"id"`
					Name         string `json:"name"`
					Difficulties []struct {
						ID   int    `json:"id"`
						Name string `json:"name"`
					} `json:"difficulties"`
				} `json:"zones"`
			} `json:"worldData"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql: %s", resp.Errors[0].Message)
	}

	var result []RelevantZone
	for _, zone := range resp.Data.WorldData.Zones {
		for _, diff := range zone.Difficulties {
			if isRelevantDifficulty(diff.Name) {
				result = append(result, RelevantZone{
					ZoneID:         zone.ID,
					ZoneName:       zone.Name,
					DifficultyID:   diff.ID,
					DifficultyName: diff.Name,
				})
			}
		}
	}
	return result, nil
}

const zoneRankingsQuery = `
query ($name: String!, $server: String!, $region: String!, $zoneID: Int!, $difficulty: Int) {
  characterData {
    character(name: $name, serverSlug: $server, serverRegion: $region) {
      zoneRankings(zoneID: $zoneID, difficulty: $difficulty)
    }
  }
}`

func (c *fflogsClient) GetZoneRankings(name, serverSlug, serverRegion string, zone RelevantZone) ([]CharacterRanking, error) {
	data, err := c.query(zoneRankingsQuery, map[string]any{
		"name":       name,
		"server":     serverSlug,
		"region":     serverRegion,
		"zoneID":     zone.ZoneID,
		"difficulty": zone.DifficultyID,
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			CharacterData struct {
				Character *struct {
					ZoneRankings json.RawMessage `json:"zoneRankings"`
				} `json:"character"`
			} `json:"characterData"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql: %s", resp.Errors[0].Message)
	}
	if resp.Data.CharacterData.Character == nil {
		return nil, fmt.Errorf("character not found on FFLogs")
	}

	var zr struct {
		Rankings []struct {
			Encounter struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"encounter"`
			RankPercent float64 `json:"rankPercent"`
		} `json:"rankings"`
	}
	if err := json.Unmarshal(resp.Data.CharacterData.Character.ZoneRankings, &zr); err != nil {
		return nil, fmt.Errorf("parsing zoneRankings: %w", err)
	}

	out := make([]CharacterRanking, 0, len(zr.Rankings))
	for _, r := range zr.Rankings {
		out = append(out, CharacterRanking{
			EncounterID:   r.Encounter.ID,
			EncounterName: r.Encounter.Name,
			RankPercent:   r.RankPercent,
		})
	}
	return out, nil
}
