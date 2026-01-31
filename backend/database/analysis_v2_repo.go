package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// AnalyzeHierarchy executes the hierarchical analysis query
func (db *DB) AnalyzeHierarchy(params AnalysisParamsV2) ([]HierarchyResult, error) {
	// 1. Build Base Query
	// We need to join glass_stats with history parquet
	historyPath := "/app/data/lake/history/**/*.parquet"

	// Determine Grouping Levels
	// Determine Grouping Levels
	// We need separate lists for CTEs (raw names) and Main Query (aliased with 'j')
	rawGroupByCols := []string{"process_code"}
	aliasedGroupByCols := []string{"j.process_code"}
	aliasedSelectCols := []string{"j.process_code"}

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
		aliasedGroupByCols = append(aliasedGroupByCols, "j.equipment_line_id")
		aliasedSelectCols = append(aliasedSelectCols, "j.equipment_line_id")
	} else {
		aliasedSelectCols = append(aliasedSelectCols, "NULL as equipment_line_id")
	}

	if targetDepth >= 2 {
		rawGroupByCols = append(rawGroupByCols, "equipment_machine_id")
		aliasedGroupByCols = append(aliasedGroupByCols, "j.equipment_machine_id")
		aliasedSelectCols = append(aliasedSelectCols, "j.equipment_machine_id")
	} else {
		aliasedSelectCols = append(aliasedSelectCols, "NULL as equipment_machine_id")
	}

	if targetDepth >= 3 {
		rawGroupByCols = append(rawGroupByCols, "equipment_path_id")
		aliasedGroupByCols = append(aliasedGroupByCols, "j.equipment_path_id")
		aliasedSelectCols = append(aliasedSelectCols, "j.equipment_path_id")
	} else {
		aliasedSelectCols = append(aliasedSelectCols, "NULL as equipment_path_id")
	}

	// 2. Build WHERE Clause
	whereClauses := []string{"1=1"}
	args := []interface{}{}

	// Validate Model Code (Required per spec)
	if params.ModelCode != "" {
		whereClauses = append(whereClauses, "g.model_code = ?")
		args = append(args, params.ModelCode)
	}

	// Date Range
	if params.Start != "" && params.End != "" {
		whereClauses = append(whereClauses, "g.work_date BETWEEN ? AND ?")
		args = append(args, params.Start, params.End)
	}

	// Defect Name
	if params.DefectName != "" {
		whereClauses = append(whereClauses, "g.defect_name = ?")
		args = append(args, params.DefectName)
	}

	// Filters on History (Optimization: Pushdown)
	if params.ProcessCode != "" {
		whereClauses = append(whereClauses, "h.process_code = ?")
		args = append(args, params.ProcessCode)
	}
	if params.EquipmentLineID != "" {
		whereClauses = append(whereClauses, "h.equipment_line_id = ?")
		args = append(args, params.EquipmentLineID)
	}
	if params.EquipmentMachineID != "" {
		whereClauses = append(whereClauses, "h.equipment_machine_id = ?")
		args = append(args, params.EquipmentMachineID)
	}

	// 3. Construct Query
	fullQuery := fmt.Sprintf(`
		WITH valid_history AS (
			SELECT * FROM read_parquet('%s', hive_partitioning=true)
		),
		joined_data AS (
			SELECT 
				g.product_id,
				g.total_defects,
				g.panel_map,
				g.panel_addrs,
				g.work_date,
				h.process_code,
				h.equipment_line_id,
				h.equipment_machine_id,
				h.equipment_path_id
			FROM glass_stats g
			JOIN valid_history h ON g.product_id = h.product_id
			WHERE %s
		),
		exploded_maps AS (
			SELECT 
				process_code, equipment_line_id, equipment_machine_id, equipment_path_id,
				UNNEST(panel_addrs) as addr,
				UNNEST(panel_map) as cnt
			FROM joined_data
		),
		map_final AS (
			SELECT
				process_code, equipment_line_id, equipment_machine_id, equipment_path_id,
				list(addr) as panel_addrs,
				list(panel_cnt) as panel_map
			FROM (
				SELECT 
					process_code, equipment_line_id, equipment_machine_id, equipment_path_id,
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
				process_code, equipment_line_id, equipment_machine_id, equipment_path_id,
				work_date,
				SUM(total_defects)::DOUBLE / COUNT(DISTINCT product_id) as dpu
			FROM joined_data
			GROUP BY %s, work_date
		),
		dpu_agg AS (
		    SELECT
		        process_code, equipment_line_id, equipment_machine_id, equipment_path_id,
		        to_json(list({'work_date': CAST(work_date as VARCHAR), 'dpu': dpu})) as trend_json
            FROM dpu_trend
            GROUP BY %s
		)
		SELECT 
			%s,
			COUNT(DISTINCT j.product_id) as total_products,
			SUM(j.total_defects) as total_defects,
			(SUM(j.total_defects)::DOUBLE / COUNT(DISTINCT j.product_id)) as dpu,
			mf.panel_addrs,
			mf.panel_map,
			da.trend_json
		FROM joined_data j
		LEFT JOIN map_final mf ON 
			(j.process_code IS NOT DISTINCT FROM mf.process_code) AND
			(j.equipment_line_id IS NOT DISTINCT FROM mf.equipment_line_id) AND
			(j.equipment_machine_id IS NOT DISTINCT FROM mf.equipment_machine_id) AND
			(j.equipment_path_id IS NOT DISTINCT FROM mf.equipment_path_id)
        LEFT JOIN dpu_agg da ON 
			(j.process_code IS NOT DISTINCT FROM da.process_code) AND
			(j.equipment_line_id IS NOT DISTINCT FROM da.equipment_line_id) AND
			(j.equipment_machine_id IS NOT DISTINCT FROM da.equipment_machine_id) AND
			(j.equipment_path_id IS NOT DISTINCT FROM da.equipment_path_id)
		GROUP BY %s, mf.panel_addrs, mf.panel_map, da.trend_json
	`,
		historyPath,
		strings.Join(whereClauses, " AND "),
		strings.Join(rawGroupByCols, ", "),     // exploded level (CTE)
		strings.Join(rawGroupByCols, ", "),     // map final level (CTE)
		strings.Join(rawGroupByCols, ", "),     // dpu level (CTE)
		strings.Join(rawGroupByCols, ", "),     // dpu agg level (CTE)
		strings.Join(aliasedSelectCols, ", "),  // main select level (j.)
		strings.Join(aliasedGroupByCols, ", ")) // main select group by (j.)

	log.Printf("Executing Analysis Query: %s [Args: %v]", fullQuery, args)

	conn, err := db.GetAnalyticsDB(params.Facility)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(fullQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []HierarchyResult
	for rows.Next() {
		var r HierarchyResult

		var panelMapStr string
		var panelAddrsStr string
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
