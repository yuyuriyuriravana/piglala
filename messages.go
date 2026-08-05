package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

const (
	templateHelp                    = "help"
	templateParseFightResult        = "parse-fight-result"
	templateStatusEmpty             = "status-empty"
	templateStatusPlayer            = "status-player"
	templateStatusNoParses          = "status-no-parses"
	templateStatusNoDisplayedParses = "status-no-displayed-parses"
	templateStatusTier              = "status-tier"
	templateStatusParse             = "status-parse"
	templateSubscribeAdded          = "subscribe-added"
	templateSubscribeAlready        = "subscribe-already"
	templateSubscribeSaveFailed     = "subscribe-save-failed"
	templateUnsubscribeMissing      = "unsubscribe-missing"
	templateUnsubscribeRemoved      = "unsubscribe-removed"
	templateUnsubscribeSaveFailed   = "unsubscribe-save-failed"
	templateWatchUsage              = "watch-usage"
	templateWatchSaveFailed         = "watch-save-failed"
	templateWatchAlready            = "watch-already"
	templateWatchAdded              = "watch-added"
	templateUnwatchUsage            = "unwatch-usage"
	templateUnwatchSaveFailed       = "unwatch-save-failed"
	templateUnwatchMissing          = "unwatch-missing"
	templateUnwatchRemoved          = "unwatch-removed"
	templatePingUsage               = "ping-usage"
	templatePingSendFailed          = "ping-send-failed"
	templatePingSent                = "ping-sent"
	templatePriceUsage              = "price-usage"
	templatePriceNotFound           = "price-not-found"
	templatePriceAmbiguous          = "price-ambiguous"
	templatePriceFetchFailed        = "price-fetch-failed"
	templatePriceResult             = "price-result"
)

var messageTemplateNames = []string{
	templateHelp,
	templateParseFightResult,
	templateStatusEmpty,
	templateStatusPlayer,
	templateStatusNoParses,
	templateStatusNoDisplayedParses,
	templateStatusTier,
	templateStatusParse,
	templateSubscribeAdded,
	templateSubscribeAlready,
	templateSubscribeSaveFailed,
	templateUnsubscribeMissing,
	templateUnsubscribeRemoved,
	templateUnsubscribeSaveFailed,
	templateWatchUsage,
	templateWatchSaveFailed,
	templateWatchAlready,
	templateWatchAdded,
	templateUnwatchUsage,
	templateUnwatchSaveFailed,
	templateUnwatchMissing,
	templateUnwatchRemoved,
	templatePingUsage,
	templatePingSendFailed,
	templatePingSent,
	templatePriceUsage,
	templatePriceNotFound,
	templatePriceAmbiguous,
	templatePriceFetchFailed,
	templatePriceResult,
}

type MessageTemplates struct {
	templates map[string]*template.Template
}

type ParseFightResultTemplateData struct {
	EncounterName string
	Table         string
	ReportURL     string
}

type PlayerTemplateData struct {
	PlayerKey string
	Name      string
	Server    string
	Region    string
}

type StatusTierTemplateData struct {
	TierName string
}

type StatusParseTemplateData struct {
	EncounterName string
	Percent       string
}

func loadMessageTemplates(dir string) (*MessageTemplates, error) {
	if dir == "" {
		return nil, fmt.Errorf("MESSAGE_TEMPLATE_DIR is required")
	}

	m := &MessageTemplates{templates: make(map[string]*template.Template, len(messageTemplateNames))}
	for _, name := range messageTemplateNames {
		path := filepath.Join(dir, name+".tmpl")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		tmpl, err := template.New(name).Option("missingkey=error").Parse(string(data))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		m.templates[name] = tmpl
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *MessageTemplates) validate() error {
	samples := map[string]any{
		templateHelp:                    emptyTemplateData{},
		templateParseFightResult:        ParseFightResultTemplateData{EncounterName: "Black Cat", Table: "Player       Job    DPS     Parse\nYuyuri Yuri  Viper  30,986  99th", ReportURL: "https://www.fflogs.com/reports/abc#fight=7&type=damage-done"},
		templateStatusEmpty:             emptyTemplateData{},
		templateStatusPlayer:            PlayerTemplateData{PlayerKey: "Yuyuri Yuri-ravana-OC", Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"},
		templateStatusNoParses:          emptyTemplateData{},
		templateStatusNoDisplayedParses: emptyTemplateData{},
		templateStatusTier:              StatusTierTemplateData{TierName: "Heavyweight (M9S-M12S)"},
		templateStatusParse:             StatusParseTemplateData{EncounterName: "Black Cat", Percent: "91st (91.0%)"},
		templateSubscribeAdded:          emptyTemplateData{},
		templateSubscribeAlready:        emptyTemplateData{},
		templateSubscribeSaveFailed:     emptyTemplateData{},
		templateUnsubscribeMissing:      emptyTemplateData{},
		templateUnsubscribeRemoved:      emptyTemplateData{},
		templateUnsubscribeSaveFailed:   emptyTemplateData{},
		templateWatchUsage:              emptyTemplateData{},
		templateWatchSaveFailed:         emptyTemplateData{},
		templateWatchAlready:            PlayerTemplateData{PlayerKey: "Yuyuri Yuri-ravana-OC", Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"},
		templateWatchAdded:              PlayerTemplateData{PlayerKey: "Yuyuri Yuri-ravana-OC", Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"},
		templateUnwatchUsage:            emptyTemplateData{},
		templateUnwatchSaveFailed:       emptyTemplateData{},
		templateUnwatchMissing:          PlayerTemplateData{PlayerKey: "Yuyuri Yuri-ravana-OC", Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"},
		templateUnwatchRemoved:          PlayerTemplateData{PlayerKey: "Yuyuri Yuri-ravana-OC", Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"},
		templatePingUsage:               emptyTemplateData{},
		templatePingSendFailed:          emptyTemplateData{},
		templatePingSent:                emptyTemplateData{},
		templatePriceUsage:              emptyTemplateData{},
		templatePriceNotFound:           PriceQueryTemplateData{Query: "dancing wings"},
		templatePriceAmbiguous:          PriceQueryTemplateData{Query: "wall shelf", Suggestions: []string{"Antique Wall Shelf", "Mounted Bookshelf"}},
		templatePriceFetchFailed:        emptyTemplateData{},
		templatePriceResult: PriceTemplateData{
			ItemName:       "Antique Wall Shelf",
			UniversalisURL: "https://universalis.app/market/35573",
			Regions: []PriceRegionTemplateData{
				{Name: "Oceania", HasListing: true, Price: "39,998", Quality: "NQ", World: "Sophia", Age: "3 days ago"},
				{Name: "Japan"},
				{Name: "Europe", Error: true},
			},
		},
	}
	for name, data := range samples {
		if _, err := m.render(name, data); err != nil {
			return fmt.Errorf("validate %s.tmpl: %w", name, err)
		}
	}
	return nil
}

func (m *MessageTemplates) Render(name string, data any) (string, error) {
	return m.render(name, data)
}

func (m *MessageTemplates) ParseFightResult(fight ParseFightResult) (string, error) {
	return m.render(templateParseFightResult, ParseFightResultTemplateData{
		EncounterName: fight.EncounterName,
		Table:         formatParseFightTable(fight.Players),
		ReportURL:     fmt.Sprintf("https://www.fflogs.com/reports/%s#fight=%d&type=damage-done", fight.ReportCode, fight.FightID),
	})
}

func formatParseFightTable(results []ParsePlayerResult) string {
	type row struct {
		player string
		job    string
		dps    string
		parse  string
	}
	rows := make([]row, 0, len(results))
	playerWidth := len("Player")
	jobWidth := len("Job")
	dpsWidth := len("DPS")
	parseWidth := len("Parse")
	for _, result := range results {
		percent := "Pending"
		if result.HasPercent {
			percent = formatParseOrdinal(result.RankPercent)
		}
		item := row{
			player: sanitizeParseTableCell(result.Player.Name),
			job:    humanizeJob(result.Job),
			dps:    formatDPS(result.Amount),
			parse:  percent,
		}
		rows = append(rows, item)
		playerWidth = max(playerWidth, len(item.player))
		jobWidth = max(jobWidth, len(item.job))
		dpsWidth = max(dpsWidth, len(item.dps))
		parseWidth = max(parseWidth, len(item.parse))
	}

	var table strings.Builder
	fmt.Fprintf(&table, "%-*s  %-*s  %*s  %-*s", playerWidth, "Player", jobWidth, "Job", dpsWidth, "DPS", parseWidth, "Parse")
	for _, item := range rows {
		fmt.Fprintf(&table, "\n%-*s  %-*s  %*s  %-*s", playerWidth, item.player, jobWidth, item.job, dpsWidth, item.dps, parseWidth, item.parse)
	}
	return table.String()
}

func sanitizeParseTableCell(value string) string {
	value = strings.ReplaceAll(value, "`", "'")
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func humanizeJob(job string) string {
	job = sanitizeParseTableCell(job)
	var out strings.Builder
	for i, r := range job {
		if i > 0 && unicode.IsUpper(r) {
			out.WriteByte(' ')
		}
		out.WriteRune(r)
	}
	return out.String()
}

func formatParseOrdinal(percent float64) string {
	n := int(percent)
	suffix := "th"
	switch n % 10 {
	case 1:
		if n%100 != 11 {
			suffix = "st"
		}
	case 2:
		if n%100 != 12 {
			suffix = "nd"
		}
	case 3:
		if n%100 != 13 {
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func (m *MessageTemplates) render(name string, data any) (string, error) {
	if m == nil {
		return "", fmt.Errorf("message templates are not configured")
	}
	tmpl := m.templates[name]
	if tmpl == nil {
		return "", fmt.Errorf("template %q is not loaded", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type emptyTemplateData struct{}
