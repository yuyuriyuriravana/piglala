package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
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
	ContentType    string
}

type ParsePlayerResult struct {
	Player      WatchedPlayer
	Job         string
	Amount      float64
	RankPercent float64
	HasPercent  bool
}

type ParseFightResult struct {
	ReportCode    string
	FightID       int
	EncounterID   int
	EncounterName string
	ZoneID        int
	DifficultyID  int
	StartedAt     time.Time
	Players       []ParsePlayerResult
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

var extremeZoneNames = map[string]bool{
	"dawntrail trials":      true,
	"endwalker trials":      true,
	"shadowbringers trials": true,
	"stormblood trials":     true,
	"heavensward trials":    true,
	"a realm reborn trials": true,
}

var ultimateZoneNames = map[string]bool{
	"futures rewritten":              true,
	"the omega protocol":             true,
	"dragonsong's reprise":           true,
	"the epic of alexander":          true,
	"the weapon's refrain":           true,
	"the unending coil of bahamut":   true,
	"the unending coil of bahamut 2": true,
	"ultimates (legacy)":             true,
	"ultimates (stormblood)":         true,
	"ultimates (shadowbringers)":     true,
	"ultimates (endwalker)":          true,
	"ultimates (dawntrail)":          true,
}

func isRelevantDifficulty(name string) bool {
	return relevantDifficultyNames[strings.ToLower(strings.TrimSpace(name))]
}

func relevantContentType(zoneName, difficultyName string) string {
	normalizedDifficulty := strings.ToLower(strings.TrimSpace(difficultyName))
	if relevantDifficultyNames[normalizedDifficulty] {
		switch normalizedDifficulty {
		case "savage":
			return "Savage"
		case "extreme":
			return "Extreme"
		case "ultimate":
			return "Ultimate"
		}
	}

	normalizedZone := normalizeZoneName(zoneName)
	switch {
	case ultimateZoneNames[normalizedZone] || strings.Contains(normalizedZone, "ultimate"):
		return "Ultimate"
	case extremeZoneNames[normalizedZone] || strings.Contains(normalizedZone, "trials"):
		return "Extreme"
	default:
		return ""
	}
}

func normalizeZoneName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
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
			contentType := relevantContentType(zone.Name, diff.Name)
			if contentType != "" {
				result = append(result, RelevantZone{
					ZoneID:         zone.ID,
					ZoneName:       zone.Name,
					DifficultyID:   diff.ID,
					DifficultyName: diff.Name,
					ContentType:    contentType,
				})
			}
		}
	}
	return result, nil
}

const recentParseRunsQuery = `
query ($name: String!, $server: String!, $region: String!, $limit: Int!) {
  characterData {
    character(name: $name, serverSlug: $server, serverRegion: $region) {
      recentReports(limit: $limit) {
        data {
          code
          startTime
          fights(killType: Kills) {
            id
            encounterID
            name
            difficulty
            startTime
          }
          rankings(compare: Parses, timeframe: Today)
        }
      }
    }
  }
}`

func (c *fflogsClient) GetRecentParseFights(player WatchedPlayer, limit int) ([]ParseFightResult, error) {
	data, err := c.query(recentParseRunsQuery, map[string]any{
		"name":   player.Name,
		"server": player.Server,
		"region": player.Region,
		"limit":  limit,
	})
	if err != nil {
		return nil, err
	}
	return decodeRecentParseFights(data)
}

func decodeRecentParseFights(data []byte) ([]ParseFightResult, error) {
	var resp struct {
		Data struct {
			CharacterData struct {
				Character *struct {
					RecentReports struct {
						Data []struct {
							Code      string  `json:"code"`
							StartTime float64 `json:"startTime"`
							Fights    []struct {
								ID            int     `json:"id"`
								EncounterID   int     `json:"encounterID"`
								EncounterName string  `json:"name"`
								DifficultyID  int     `json:"difficulty"`
								StartTime     float64 `json:"startTime"`
							} `json:"fights"`
							Rankings json.RawMessage `json:"rankings"`
						} `json:"data"`
					} `json:"recentReports"`
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

	var out []ParseFightResult
	for _, report := range resp.Data.CharacterData.Character.RecentReports.Data {
		fightStarts := make(map[int]time.Time, len(report.Fights))
		for _, fight := range report.Fights {
			absoluteMillis := int64(math.Round(report.StartTime + fight.StartTime))
			fightStarts[fight.ID] = time.UnixMilli(absoluteMillis).UTC()
		}

		var rankings struct {
			Data []struct {
				FightID   int `json:"fightID"`
				ZoneID    int `json:"zone"`
				Encounter struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"encounter"`
				DifficultyID int `json:"difficulty"`
				Roles        map[string]struct {
					Characters []struct {
						Name   string  `json:"name"`
						Class  string  `json:"class"`
						Amount float64 `json:"amount"`
						Server struct {
							Name   string `json:"name"`
							Region string `json:"region"`
						} `json:"server"`
						RankPercent json.RawMessage `json:"rankPercent"`
					} `json:"characters"`
				} `json:"roles"`
			} `json:"data"`
		}
		if len(report.Rankings) == 0 || string(report.Rankings) == "null" {
			continue
		}
		if err := json.Unmarshal(report.Rankings, &rankings); err != nil {
			return nil, fmt.Errorf("parsing report %s rankings: %w", report.Code, err)
		}
		for _, ranking := range rankings.Data {
			startedAt, ok := fightStarts[ranking.FightID]
			if !ok {
				continue
			}
			fight := ParseFightResult{
				ReportCode:    report.Code,
				FightID:       ranking.FightID,
				EncounterID:   ranking.Encounter.ID,
				EncounterName: ranking.Encounter.Name,
				ZoneID:        ranking.ZoneID,
				DifficultyID:  ranking.DifficultyID,
				StartedAt:     startedAt,
			}
			for _, role := range ranking.Roles {
				for _, character := range role.Characters {
					if strings.TrimSpace(character.Name) == "" || strings.TrimSpace(character.Server.Name) == "" || strings.TrimSpace(character.Server.Region) == "" {
						continue
					}
					percent, hasPercent := decodeRankingPercent(character.RankPercent)
					fight.Players = append(fight.Players, ParsePlayerResult{
						Player: WatchedPlayer{
							Name:   strings.TrimSpace(character.Name),
							Server: strings.ToLower(strings.TrimSpace(character.Server.Name)),
							Region: strings.ToUpper(strings.TrimSpace(character.Server.Region)),
						},
						Job:         character.Class,
						Amount:      character.Amount,
						RankPercent: percent,
						HasPercent:  hasPercent,
					})
				}
			}
			if len(fight.Players) > 0 {
				out = append(out, fight)
			}
		}
	}
	return out, nil
}

func sameFFLogsCharacter(player WatchedPlayer, name, server, region string) bool {
	return strings.EqualFold(strings.TrimSpace(player.Name), strings.TrimSpace(name)) &&
		strings.EqualFold(strings.TrimSpace(player.Server), strings.TrimSpace(server)) &&
		strings.EqualFold(strings.TrimSpace(player.Region), strings.TrimSpace(region))
}

func decodeRankingPercent(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return value, err == nil
}
