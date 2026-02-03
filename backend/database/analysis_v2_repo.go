package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// AnalyzeHierarchy executes the hierarchical analysis query
func (db *DB) AnalyzeHierarchy(params AnalysisParamsV2) ([]HierarchyResult, error) {
	// 1. Build Base Query
	// We query directly from glass_stats (Pre-aggregated Mart)

	// Determine Grouping Levels
	// Determine Grouping Levels
	// We need separate lists for CTEs (raw names) and Main Query (aliased with 'p' for product_agg)
	rawGroupByCols := []string{"process_code"}
	aliasedGroupByCols := []string{"p.process_code"}
	aliasedSelectCols := []string{"p.process_code"}

	// Dynamic CTE Selects (raw names, but with NULLs for missing levels)
	cteSelectCols := []string{"process_code"}

	levels := map[string]int{
		"process": 0,
		"line":    1,
		"machine": 2,
		"path":    3,
	}

	targetLevel := "path" // default
	if params.AnalysisLevel != "" {
		targetLevel = params.AnalysisLevel
	} else if params.EquipmentPathID != "" {
		targetLevel = "path"
	} else if params.EquipmentMachineID != "" {
		targetLevel = "machine"
	} else if params.EquipmentLineID != "" {
		targetLevel = "line"
	} else if params.ProcessCode != "" {
		targetLevel = "path"
	}

	targetDepth := levels[targetLevel]

	if targetDepth >= 1 {
		rawGroupByCols = append(rawGroupByCols, "equipment_line_id")
		aliasedGroupByCols = append(aliasedGroupByCols, "p.equipment_line_id")
		aliasedSelectCols = append(aliasedSelectCols, "p.equipment_line_id")
		cteSelectCols = append(cteSelectCols, "equipment_line_id")
	} else {
		aliasedSelectCols = append(aliasedSelectCols, "NULL as equipment_line_id")
		cteSelectCols = append(cteSelectCols, "NULL as equipment_line_id")
	}

	if targetDepth >= 2 {
		rawGroupByCols = append(rawGroupByCols, "equipment_machine_id")
		aliasedGroupByCols = append(aliasedGroupByCols, "p.equipment_machine_id")
		aliasedSelectCols = append(aliasedSelectCols, "p.equipment_machine_id")
		cteSelectCols = append(cteSelectCols, "equipment_machine_id")
	} else {
		aliasedSelectCols = append(aliasedSelectCols, "NULL as equipment_machine_id")
		cteSelectCols = append(cteSelectCols, "NULL as equipment_machine_id")
	}

	if targetDepth >= 3 {
		rawGroupByCols = append(rawGroupByCols, "equipment_path_id")
		aliasedGroupByCols = append(aliasedGroupByCols, "p.equipment_path_id")
		aliasedSelectCols = append(aliasedSelectCols, "p.equipment_path_id")
		cteSelectCols = append(cteSelectCols, "equipment_path_id")
	} else {
		aliasedSelectCols = append(aliasedSelectCols, "NULL as equipment_path_id")
		cteSelectCols = append(cteSelectCols, "NULL as equipment_path_id")
	}

	// 2. Build WHERE Clause
	whereClauses := []string{"1=1"}
	args := []interface{}{}

	// Validate Model Code (Required per spec)
	if params.ModelCode != "" {
		log.Printf("[DEBUG] ModelCode Raw: %q Hex: %x", params.ModelCode, []byte(params.ModelCode))
		whereClauses = append(whereClauses, "g.model_code = ?")
		args = append(args, params.ModelCode)
	}

	// Date Range
	if params.Start != "" && params.End != "" {
		dateCol := "g.inspection_time" // Default
		if params.DateType == "work" {
			dateCol = "g.work_time"
		}
		whereClauses = append(whereClauses, fmt.Sprintf("%s BETWEEN CAST(? AS DATE) AND CAST(? AS TIMESTAMP)", dateCol))

		// Note: work_time is TIMESTAMP, inspection_time is TIMESTAMP.
		// If inspection_time, we might need to cast range start/end to TIMESTAMP or cast col to DATE.
		// Let's safe cast the column to DATE for comparison to match YYYY-MM-DD input.
		if params.DateType == "work" {
			whereClauses[len(whereClauses)-1] = "CAST(g.work_time AS DATE) BETWEEN CAST(? AS DATE) AND CAST(? AS DATE)"
		} else {
			// inspection_time is TIMESTAMP. Input is YYYY-MM-DD.
			// CAST(inspection_time AS DATE) BETWEEN ? AND ?
			whereClauses[len(whereClauses)-1] = "CAST(g.inspection_time AS DATE) BETWEEN CAST(? AS DATE) AND CAST(? AS DATE)"
		}
		args = append(args, params.Start, params.End)
	}

	// Defect Name
	if params.DefectName != "" {
		// Normalize to NFC (standard for Linux/Golang/DuckDB)
		normalizedDefect := norm.NFC.String(params.DefectName)
		log.Printf("[DEBUG] DefectName Raw: %q Hex: %x | Normalized: %q Hex: %x",
			params.DefectName, []byte(params.DefectName),
			normalizedDefect, []byte(normalizedDefect))

		whereClauses = append(whereClauses, "g.defect_name = ?")
		args = append(args, normalizedDefect)
	}

	// Filters on Hierarchy (Now in glass_stats)
	if params.ProcessCode != "" {
		whereClauses = append(whereClauses, "g.process_code = ?")
		args = append(args, params.ProcessCode)
	}
	if params.EquipmentLineID != "" {
		whereClauses = append(whereClauses, "g.equipment_line_id = ?")
		args = append(args, params.EquipmentLineID)
	}
	if params.EquipmentMachineID != "" {
		whereClauses = append(whereClauses, "g.equipment_machine_id = ?")
		args = append(args, params.EquipmentMachineID)
	}

	// Get Connection Early for Debugging
	conn, err := db.GetAnalyticsDB(params.Facility)
	if err != nil {
		return nil, err
	}

	// 3. Construct Query
	cteSelectStr := strings.Join(cteSelectCols, ", ")

	// Build Dynamic Join Conditions
	var joinConditions []string
	joinConditions = append(joinConditions, "(p.process_code IS NOT DISTINCT FROM mf.process_code)")

	if targetDepth >= 1 {
		joinConditions = append(joinConditions, "(p.equipment_line_id IS NOT DISTINCT FROM mf.equipment_line_id)")
	}
	if targetDepth >= 2 {
		joinConditions = append(joinConditions, "(p.equipment_machine_id IS NOT DISTINCT FROM mf.equipment_machine_id)")
	}
	if targetDepth >= 3 {
		joinConditions = append(joinConditions, "(p.equipment_path_id IS NOT DISTINCT FROM mf.equipment_path_id)")
	}

	joinClause := strings.Join(joinConditions, " AND ")

	// DPU Agg Join (Same conditions)
	dpuJoinClause := strings.ReplaceAll(joinClause, "mf.", "da.")

	fullQuery := fmt.Sprintf(`
		WITH joined_data AS (
			SELECT 
				g.product_id,
				g.total_defects,
				g.panel_map,
				g.panel_addrs,
				g.work_time,
				g.process_code,
				g.equipment_line_id,
				g.equipment_machine_id,
				g.equipment_path_id
			FROM glass_stats g
			WHERE %s
		),
		product_agg AS (
			SELECT
				%s,
				COUNT(DISTINCT product_id) as total_products,
				SUM(total_defects) as total_defects,
				(SUM(total_defects)::DOUBLE / COUNT(DISTINCT product_id)) as dpu
			FROM joined_data
			GROUP BY %s
		),
		exploded_maps AS (
			SELECT 
				%s,
				UNNEST(panel_addrs) as addr,
				UNNEST(panel_map) as cnt
			FROM joined_data
		),
		map_final AS (
			SELECT
				%s,
				CAST(to_json(list(addr)) AS VARCHAR) as panel_addrs,
				CAST(to_json(list(panel_cnt)) AS VARCHAR) as panel_map
			FROM (
				SELECT 
					%s,
					addr,
					CAST(SUM(cnt) AS INTEGER) as panel_cnt
				FROM exploded_maps
				GROUP BY %s, addr
				ORDER BY addr
			) sub
			GROUP BY %s
		),
		dpu_trend AS (
			SELECT
				%s,
				CAST(work_time AS DATE) as work_date,
				SUM(total_defects)::DOUBLE / COUNT(DISTINCT product_id) as dpu
			FROM joined_data
			GROUP BY %s, CAST(work_time AS DATE)
		),
		dpu_agg AS (
		    SELECT
		        %s,
		        CAST(to_json(list({'work_date': CAST(work_date as VARCHAR), 'dpu': dpu})) AS VARCHAR) as trend_json
            FROM dpu_trend
            GROUP BY %s
		)
		SELECT 
			%s,
			p.total_products,
			p.total_defects,
			p.dpu,
			mf.panel_addrs,
			mf.panel_map,
			da.trend_json
		FROM product_agg p
		LEFT JOIN map_final mf ON %s
        LEFT JOIN dpu_agg da ON %s
	`,
		strings.Join(whereClauses, " AND "),   // WHERE clause
		cteSelectStr,                          // product_agg SELECT
		strings.Join(rawGroupByCols, ", "),    // product_agg GROUP BY
		cteSelectStr,                          // exploded_maps SELECT
		cteSelectStr,                          // map_final SELECT
		cteSelectStr,                          // map_final subquery SELECT
		strings.Join(rawGroupByCols, ", "),    // map_final subquery GROUP BY
		strings.Join(rawGroupByCols, ", "),    // map_final GROUP BY
		cteSelectStr,                          // dpu_trend SELECT
		strings.Join(rawGroupByCols, ", "),    // dpu_trend GROUP BY
		cteSelectStr,                          // dpu_agg SELECT
		strings.Join(rawGroupByCols, ", "),    // dpu_agg GROUP BY
		strings.Join(aliasedSelectCols, ", "), // main SELECT (p.)
		joinClause,                            // JOIN mf condition
		dpuJoinClause)                         // JOIN da condition

	log.Printf("Executing Analysis Query: %s [Args: %v]", fullQuery, args)

	rows, err := conn.Query(fullQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []HierarchyResult
	for rows.Next() {
		var r HierarchyResult

		var panelMapStr sql.NullString
		var panelAddrsStr sql.NullString
		var trendJson string

		var eqLine sql.NullString
		var eqMach sql.NullString
		var eqPath sql.NullString

		if err := rows.Scan(
			&r.ProcessCode,
			&eqLine, &eqMach, &eqPath,
			&r.TotalProducts,
			&r.TotalDefects,
			&r.DPU,
			&panelAddrsStr,
			&panelMapStr,
			&trendJson,
		); err != nil {
			return nil, err
		}

		r.EquipmentLineID = eqLine.String
		r.EquipmentMachineID = eqMach.String
		r.EquipmentPathID = eqPath.String

		// Parse Lists (JSON strings from DuckDB)
		if panelMapStr.Valid {

			if err := json.Unmarshal([]byte(panelMapStr.String), &r.PanelMap); err != nil {
				log.Printf("[Results] Failed to parse PanelMap: %v", err)
			}
		}
		if panelAddrsStr.Valid {
			if err := json.Unmarshal([]byte(panelAddrsStr.String), &r.PanelAddrs); err != nil {
				log.Printf("[Results] Failed to parse PanelAddrs: %v", err)
			}
		}
		if trendJson != "" {
			if err := json.Unmarshal([]byte(trendJson), &r.DailyDPU); err != nil {
				log.Printf("[Results] Failed to parse DailyDPU: %v", err)
			}
		}

		results = append(results, r)
	}

	return results, nil
}
