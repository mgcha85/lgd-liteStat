package database

import (
	"database/sql"
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

	// --- DEBUG: Granular Verification ---
	// 0. Check DB Connection & Total Count
	var totalCount int
	if err := conn.QueryRow("SELECT COUNT(*) FROM glass_stats").Scan(&totalCount); err != nil {
		log.Printf("[DEBUG] FATAL: Could not query glass_stats: %v", err)
	} else {
		log.Printf("[DEBUG] Total Rows in glass_stats: %d", totalCount)
	}

	// 1. Check Date Match
	var dateCount int
	// Re-construct filter args independently for debug
	// Date
	dateFilter := fmt.Sprintf("%s BETWEEN CAST(? AS DATE) AND CAST(? AS TIMESTAMP)", "g.inspection_time")
	if params.DateType == "work" {
		dateFilter = "CAST(g.work_time AS DATE) BETWEEN CAST(? AS DATE) AND CAST(? AS DATE)"
	} else {
		dateFilter = "CAST(g.inspection_time AS DATE) BETWEEN CAST(? AS DATE) AND CAST(? AS DATE)"
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM glass_stats g WHERE "+dateFilter, params.Start, params.End).Scan(&dateCount); err != nil {
		log.Printf("[DEBUG] Date Filter Error: %v", err)
	} else {
		log.Printf("[DEBUG] Rows matching Date [%s ~ %s]: %d", params.Start, params.End, dateCount)
	}

	// 2. Check Model Match
	if params.ModelCode != "" {
		var modelCount int
		if err := conn.QueryRow("SELECT COUNT(*) FROM glass_stats g WHERE g.model_code = ?", params.ModelCode).Scan(&modelCount); err != nil {
			log.Printf("[DEBUG] Model Filter Error: %v", err)
		} else {
			log.Printf("[DEBUG] Rows matching Model [%s]: %d", params.ModelCode, modelCount)
		}
	}

	// 3. Check Defect Match
	if params.DefectName != "" {
		var defectCount int
		normalizedDefect := norm.NFC.String(params.DefectName)
		if err := conn.QueryRow("SELECT COUNT(*) FROM glass_stats g WHERE g.defect_name = ?", normalizedDefect).Scan(&defectCount); err != nil {
			log.Printf("[DEBUG] Defect Filter Error: %v", err)
		} else {
			log.Printf("[DEBUG] Rows matching Defect [%s]: %d", normalizedDefect, defectCount)
		}
	}

	// 4. Combined (Original debug)
	debugQuery := fmt.Sprintf("SELECT COUNT(*) FROM glass_stats g WHERE %s", strings.Join(whereClauses, " AND "))
	var finalCount int
	if err := conn.QueryRow(debugQuery, args...).Scan(&finalCount); err != nil {
		log.Printf("[DEBUG] Failed to count combined rows: %v", err)
	} else {
		log.Printf("[DEBUG] COMBINED Filtered Source Rows: %d", finalCount)
	}
	// -----------------------------

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
					SUM(cnt) as panel_cnt
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

	// === DEBUG: Check CTE Row Counts ===
	// Check joined_data count
	debugJoinedDataQuery := fmt.Sprintf(`
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
		)
		SELECT COUNT(*) FROM joined_data
	`, strings.Join(whereClauses, " AND "))

	var joinedDataCount int
	if err := conn.QueryRow(debugJoinedDataQuery, args...).Scan(&joinedDataCount); err != nil {
		log.Printf("[DEBUG] Failed to count joined_data: %v", err)
	} else {
		log.Printf("[DEBUG] joined_data CTE rows: %d", joinedDataCount)
	}

	// Check exploded_maps count (only if joined_data has rows)
	if joinedDataCount > 0 {
		debugExplodedQuery := fmt.Sprintf(`
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
			exploded_maps AS (
				SELECT 
					%s,
					UNNEST(panel_addrs) as addr,
					UNNEST(panel_map) as cnt
				FROM joined_data
			)
			SELECT COUNT(*) FROM exploded_maps
		`, strings.Join(whereClauses, " AND "), cteSelectStr)

		var explodedCount int
		if err := conn.QueryRow(debugExplodedQuery, args...).Scan(&explodedCount); err != nil {
			log.Printf("[DEBUG] Failed to count exploded_maps: %v", err)
		} else {
			log.Printf("[DEBUG] exploded_maps CTE rows: %d", explodedCount)
		}

		// Check map_final count
		debugMapFinalQuery := fmt.Sprintf(`
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
						SUM(cnt) as panel_cnt
					FROM exploded_maps
					GROUP BY %s, addr
					ORDER BY addr
				) sub
				GROUP BY %s
			)
			SELECT COUNT(*) FROM map_final
		`, strings.Join(whereClauses, " AND "), cteSelectStr, cteSelectStr, cteSelectStr,
			strings.Join(rawGroupByCols, ", "), strings.Join(rawGroupByCols, ", "))

		var mapFinalCount int
		if err := conn.QueryRow(debugMapFinalQuery, args...).Scan(&mapFinalCount); err != nil {
			log.Printf("[DEBUG] Failed to count map_final: %v", err)
		} else {
			log.Printf("[DEBUG] map_final CTE rows: %d", mapFinalCount)
		}

		// Check dpu_agg count
		debugDpuAggQuery := fmt.Sprintf(`
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
			SELECT COUNT(*) FROM dpu_agg
		`, strings.Join(whereClauses, " AND "), cteSelectStr, strings.Join(rawGroupByCols, ", "),
			cteSelectStr, strings.Join(rawGroupByCols, ", "))

		var dpuAggCount int
		if err := conn.QueryRow(debugDpuAggQuery, args...).Scan(&dpuAggCount); err != nil {
			log.Printf("[DEBUG] Failed to count dpu_agg: %v", err)
		} else {
			log.Printf("[DEBUG] dpu_agg CTE rows: %d", dpuAggCount)
		}

		// Log JOIN conditions
		log.Printf("[DEBUG] JOIN conditions (map_final): %s", joinClause)
		log.Printf("[DEBUG] JOIN conditions (dpu_agg): %s", dpuJoinClause)

		// === RAW DATA DEBUG ===
		debugRaw := fmt.Sprintf(`SELECT g.process_code, g.equipment_line_id, g.product_id FROM glass_stats g WHERE %s LIMIT 3`, strings.Join(whereClauses, " AND "))
		rawRows, _ := conn.Query(debugRaw, args...)
		if rawRows != nil {
			defer rawRows.Close()
			log.Printf("[DEBUG] RAW glass_stats data:")
			for rawRows.Next() {
				var proc, line, prod sql.NullString
				rawRows.Scan(&proc, &line, &prod)
				log.Printf("  RAW: process=%s, line=%s, product=%s", proc.String, line.String, prod.String)
			}
		}
		// === END RAW DATA DEBUG ===

		// === ADDITIONAL DEBUG: Sample Keys ===
		//Show sample keys from joined_data
		debugKeysQuery := fmt.Sprintf(`
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
			)
			SELECT DISTINCT %s
			FROM joined_data
			LIMIT 3
		`, strings.Join(whereClauses, " AND "), cteSelectStr)

		keyRows, err := conn.Query(debugKeysQuery, args...)
		if err == nil {
			defer keyRows.Close()
			log.Printf("[DEBUG] Sample keys from joined_data:")
			for keyRows.Next() {
				// Scan based on targetDepth
				var procCode string
				var eqLine, eqMach, eqPath sql.NullString

				if targetDepth >= 3 {
					keyRows.Scan(&procCode, &eqLine, &eqMach, &eqPath)
				} else if targetDepth >= 2 {
					keyRows.Scan(&procCode, &eqLine, &eqMach)
				} else if targetDepth >= 1 {
					keyRows.Scan(&procCode, &eqLine)
				} else {
					keyRows.Scan(&procCode)
				}

				log.Printf("  -> process=%s, line=%v, machine=%v, path=%v",
					procCode, eqLine.String, eqMach.String, eqPath.String)
			}
		}

		// Show sample keys from map_final
		debugMapKeysQuery := fmt.Sprintf(`
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
						SUM(cnt) as panel_cnt
					FROM exploded_maps
					GROUP BY %s, addr
					ORDER BY addr
				) sub
				GROUP BY %s
			)
			SELECT %s FROM map_final LIMIT 3
		`, strings.Join(whereClauses, " AND "), cteSelectStr, cteSelectStr, cteSelectStr,
			strings.Join(rawGroupByCols, ", "), strings.Join(rawGroupByCols, ", "), cteSelectStr)

		mapKeyRows, err := conn.Query(debugMapKeysQuery, args...)
		if err == nil {
			defer mapKeyRows.Close()
			log.Printf("[DEBUG] Sample keys from map_final:")
			for mapKeyRows.Next() {
				var procCode string
				var eqLine, eqMach, eqPath sql.NullString

				if targetDepth >= 3 {
					mapKeyRows.Scan(&procCode, &eqLine, &eqMach, &eqPath)
				} else if targetDepth >= 2 {
					mapKeyRows.Scan(&procCode, &eqLine, &eqMach)
				} else if targetDepth >= 1 {
					mapKeyRows.Scan(&procCode, &eqLine)
				} else {
					mapKeyRows.Scan(&procCode)
				}

				log.Printf("  -> process=%s, line=%v, machine=%v, path=%v",
					procCode, eqLine.String, eqMach.String, eqPath.String)
			}
		}
	}
	// === END DEBUG ===

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

		// TODO: Parse Lists
		// r.PanelMap = parseDuckDBIntList(panelMapStr)
		// r.PanelAddrs = parseDuckDBStringList(panelAddrsStr)
		// r.DailyDPU = parseTrendJson(trendJson)

		results = append(results, r)
	}

	return results, nil
}
