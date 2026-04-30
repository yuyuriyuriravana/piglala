package main

import (
	"sort"
	"strings"
)

type encounterOrder struct {
	tierName     string
	tierOrder    int
	floorOrder   int
	phaseOrder   int
	showInStatus bool
}

const ultimatesTierName = "Ultimates"

var ultimateEncounterNames = map[string]bool{
	"the unending coil of bahamut":            true,
	"the unending coil of bahamut (ultimate)": true,
	"the weapon's refrain":                    true,
	"the weapon's refrain (ultimate)":         true,
	"the epic of alexander":                   true,
	"the epic of alexander (ultimate)":        true,
	"dragonsong's reprise":                    true,
	"dragonsong's reprise (ultimate)":         true,
	"the omega protocol":                      true,
	"the omega protocol (ultimate)":           true,
	"futures rewritten":                       true,
	"futures rewritten (ultimate)":            true,
}

var ultimateEncounterOrder = map[string]int{
	"the unending coil of bahamut": 1,
	"the weapon's refrain":         2,
	"the epic of alexander":        3,
	"dragonsong's reprise":         4,
	"the omega protocol":           5,
	"futures rewritten":            6,
}

var legacyUltimateEncounterIDs = map[int]bool{
	1060: true,
	1061: true,
	1062: true,
	1065: true,
	1068: true,
	1072: true,
}

var canonicalUltimateEncounterIDs = map[string]int{
	"the unending coil of bahamut": 1073,
	"futures rewritten":            1079,
}

var encounterOrders = map[int]encounterOrder{
	1060: {tierName: ultimatesTierName, tierOrder: 900, floorOrder: 1, showInStatus: true}, // UCoB
	1061: {tierName: ultimatesTierName, tierOrder: 900, floorOrder: 2, showInStatus: true}, // UWU
	1062: {tierName: ultimatesTierName, tierOrder: 900, floorOrder: 3, showInStatus: true}, // TEA
	1065: {tierName: ultimatesTierName, tierOrder: 900, floorOrder: 4, showInStatus: true}, // DSR
	1068: {tierName: ultimatesTierName, tierOrder: 900, floorOrder: 5, showInStatus: true}, // TOP
	1072: {tierName: ultimatesTierName, tierOrder: 900, floorOrder: 6, showInStatus: true}, // FRU
	78:   {tierName: "Asphodelos (P1S-P4S)", tierOrder: 10, floorOrder: 1},
	79:   {tierName: "Asphodelos (P1S-P4S)", tierOrder: 10, floorOrder: 2},
	80:   {tierName: "Asphodelos (P1S-P4S)", tierOrder: 10, floorOrder: 3},
	81:   {tierName: "Asphodelos (P1S-P4S)", tierOrder: 10, floorOrder: 4},
	82:   {tierName: "Asphodelos (P1S-P4S)", tierOrder: 10, floorOrder: 4, phaseOrder: 1},
	83:   {tierName: "Abyssos (P5S-P8S)", tierOrder: 20, floorOrder: 1},
	84:   {tierName: "Abyssos (P5S-P8S)", tierOrder: 20, floorOrder: 2},
	85:   {tierName: "Abyssos (P5S-P8S)", tierOrder: 20, floorOrder: 3},
	86:   {tierName: "Abyssos (P5S-P8S)", tierOrder: 20, floorOrder: 4},
	87:   {tierName: "Abyssos (P5S-P8S)", tierOrder: 20, floorOrder: 4, phaseOrder: 1},
	88:   {tierName: "Anabaseios (P9S-P12S)", tierOrder: 30, floorOrder: 1},
	89:   {tierName: "Anabaseios (P9S-P12S)", tierOrder: 30, floorOrder: 2},
	90:   {tierName: "Anabaseios (P9S-P12S)", tierOrder: 30, floorOrder: 3},
	91:   {tierName: "Anabaseios (P9S-P12S)", tierOrder: 30, floorOrder: 4},
	92:   {tierName: "Anabaseios (P9S-P12S)", tierOrder: 30, floorOrder: 4, phaseOrder: 1},
	93:   {tierName: "Light-heavyweight (M1S-M4S)", tierOrder: 40, floorOrder: 1},
	94:   {tierName: "Light-heavyweight (M1S-M4S)", tierOrder: 40, floorOrder: 2},
	95:   {tierName: "Light-heavyweight (M1S-M4S)", tierOrder: 40, floorOrder: 3},
	96:   {tierName: "Light-heavyweight (M1S-M4S)", tierOrder: 40, floorOrder: 4},
	97:   {tierName: "Cruiserweight (M5S-M8S)", tierOrder: 50, floorOrder: 1},
	98:   {tierName: "Cruiserweight (M5S-M8S)", tierOrder: 50, floorOrder: 2},
	99:   {tierName: "Cruiserweight (M5S-M8S)", tierOrder: 50, floorOrder: 3},
	100:  {tierName: "Cruiserweight (M5S-M8S)", tierOrder: 50, floorOrder: 4},
	101:  {tierName: "Heavyweight (M9S-M12S)", tierOrder: 60, floorOrder: 1, showInStatus: true},
	102:  {tierName: "Heavyweight (M9S-M12S)", tierOrder: 60, floorOrder: 2, showInStatus: true},
	103:  {tierName: "Heavyweight (M9S-M12S)", tierOrder: 60, floorOrder: 3, showInStatus: true},
	104:  {tierName: "Heavyweight (M9S-M12S)", tierOrder: 60, floorOrder: 4, showInStatus: true},
	105:  {tierName: "Heavyweight (M9S-M12S)", tierOrder: 60, floorOrder: 4, phaseOrder: 1, showInStatus: true},
}

type orderedBestParse struct {
	encounterID int
	best        BestParse
	order       encounterOrder
}

func writeBestParseStatus(sb *strings.Builder, bests map[int]BestParse, messages *MessageTemplates) (bool, error) {
	var currentTier string
	entries := orderedBestParses(bests)
	for _, entry := range entries {
		if entry.order.tierName != currentTier {
			currentTier = entry.order.tierName
			line, err := messages.Render(templateStatusTier, StatusTierTemplateData{TierName: currentTier})
			if err != nil {
				return false, err
			}
			sb.WriteString(line)
		}
		line, err := messages.Render(templateStatusParse, StatusParseTemplateData{
			EncounterName: entry.best.EncounterName,
			Percent:       formatPct(entry.best.RankPercent),
		})
		if err != nil {
			return false, err
		}
		sb.WriteString(line)
	}
	return len(entries) > 0, nil
}

func orderedBestParses(bests map[int]BestParse) []orderedBestParse {
	out := make([]orderedBestParse, 0, len(bests))
	ultimateIndexes := map[string]int{}
	for encounterID, best := range bests {
		order, ok := encounterOrders[encounterID]
		if ok && order.tierName == ultimatesTierName {
			if !isUltimateEncounter(best.EncounterName) {
				continue
			}
			order.floorOrder = ultimateOrder(best.EncounterName, order.floorOrder)
		} else if !ok || !order.showInStatus {
			if !isUltimateEncounter(best.EncounterName) {
				continue
			}
			order = ultimateStatusOrder(best.EncounterName, encounterID)
		}

		entry := orderedBestParse{
			encounterID: encounterID,
			best:        best,
			order:       order,
		}
		if order.tierName == ultimatesTierName {
			nameKey := normalizeEncounterName(best.EncounterName)
			if existing, exists := ultimateIndexes[nameKey]; exists {
				if preferUltimateEntry(entry, out[existing]) {
					out[existing] = entry
				}
				continue
			}
			ultimateIndexes[nameKey] = len(out)
		}
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool {
		a := out[i]
		b := out[j]
		if a.order.tierOrder != b.order.tierOrder {
			return a.order.tierOrder < b.order.tierOrder
		}
		if a.order.floorOrder != b.order.floorOrder {
			return a.order.floorOrder < b.order.floorOrder
		}
		if a.order.phaseOrder != b.order.phaseOrder {
			return a.order.phaseOrder < b.order.phaseOrder
		}
		return a.encounterID < b.encounterID
	})

	return out
}

func isUltimateEncounter(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(normalized, "ultimate") {
		return true
	}
	normalized = normalizeEncounterName(normalized)
	if ultimateEncounterNames[normalized] {
		return true
	}
	return false
}

func normalizeEncounterName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, " (ultimate)")
	return strings.TrimSpace(name)
}

func ultimateStatusOrder(name string, fallback int) encounterOrder {
	return encounterOrder{
		tierName:     ultimatesTierName,
		tierOrder:    900,
		floorOrder:   ultimateOrder(name, fallback),
		showInStatus: true,
	}
}

func ultimateOrder(name string, fallback int) int {
	if order, ok := ultimateEncounterOrder[normalizeEncounterName(name)]; ok {
		return order
	}
	return fallback
}

func preferUltimateEntry(candidate, current orderedBestParse) bool {
	candidateName := normalizeEncounterName(candidate.best.EncounterName)
	currentName := normalizeEncounterName(current.best.EncounterName)
	if candidateName != currentName {
		return false
	}

	if canonicalID, ok := canonicalUltimateEncounterIDs[candidateName]; ok {
		if candidate.encounterID == canonicalID && current.encounterID != canonicalID {
			return true
		}
		if current.encounterID == canonicalID && candidate.encounterID != canonicalID {
			return false
		}
	}

	if legacyUltimateEncounterIDs[candidate.encounterID] != legacyUltimateEncounterIDs[current.encounterID] {
		return !legacyUltimateEncounterIDs[candidate.encounterID]
	}

	if candidate.encounterID != current.encounterID {
		return candidate.encounterID > current.encounterID
	}
	return candidate.best.RankPercent > current.best.RankPercent
}
