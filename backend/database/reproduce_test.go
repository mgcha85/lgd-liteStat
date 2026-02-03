package database

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "github.com/marcboeker/go-duckdb"
)

func TestReproduceHierarchy(t *testing.T) {
	// 1. Setup In-Memory DB
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer db.Close()

	// 2. Create Schema
	_, err = db.Exec(`
		CREATE TABLE glass_stats (
			product_id TEXT,
			defect_name TEXT,
			model_code TEXT,
			work_time TIMESTAMP,
			process_code TEXT,
			equipment_line_id TEXT,
			equipment_machine_id TEXT,
			equipment_path_id TEXT,
			total_defects INTEGER,
			panel_map INTEGER[],
			panel_addrs TEXT[]
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 3. Insert Mock Data
	// Using raw SQL for simplicity.
	// Group 1: Normal (P1, LineA) -> Should have maps
	_, err = db.Exec(`
		INSERT INTO glass_stats VALUES 
		('PROD_1', 'Defect_A', 'GS', '2026-01-01 10:00:00', 'PROC_1', 'LINE_A', 'MACH_1', 'PATH_1', 10, [1, 2], ['A1', 'A2']),
		('PROD_2', 'Defect_A', 'GS', '2026-01-01 11:00:00', 'PROC_1', 'LINE_A', 'MACH_1', 'PATH_1', 5,  [3],    ['B1'])
	`)
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	// Group 2: Empty Addrs (P2, LineB) -> Should have NULL maps but valid DPU
	_, err = db.Exec(`
		INSERT INTO glass_stats VALUES 
		('PROD_4', 'Defect_A', 'GS', '2026-01-01 12:00:00', 'PROC_2', 'LINE_B', 'MACH_2', 'PATH_2', 20, [],     []),
		('PROD_5', 'Defect_A', 'GS', '2026-01-01 13:00:00', 'PROC_2', 'LINE_B', 'MACH_2', 'PATH_2', 15, [],     [])
	`)
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	// 4. Run Analysis Query
	// Matches strict targetDepth=3 logic
	cteCols := "process_code, equipment_line_id, equipment_machine_id, equipment_path_id"
	groupByCols := "process_code, equipment_line_id, equipment_machine_id, equipment_path_id"
	aliasedSelect := "p.process_code, p.equipment_line_id, p.equipment_machine_id, p.equipment_path_id"

	// JOIN Conditions
	joinCond := `(p.process_code IS NOT DISTINCT FROM mf.process_code) AND 
				 (p.equipment_line_id IS NOT DISTINCT FROM mf.equipment_line_id) AND 
				 (p.equipment_machine_id IS NOT DISTINCT FROM mf.equipment_machine_id) AND 
				 (p.equipment_path_id IS NOT DISTINCT FROM mf.equipment_path_id)`
	dpuJoinCond := strings.ReplaceAll(joinCond, "mf.", "da.")

	query := fmt.Sprintf(`
		WITH joined_data AS (
			SELECT 
				g.product_id, g.total_defects, g.panel_map, g.panel_addrs, g.work_time,
				g.process_code, g.equipment_line_id, g.equipment_machine_id, g.equipment_path_id
			FROM glass_stats g
			WHERE 1=1
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
			%s,  -- p.process_code ...
			p.total_products,
			p.total_defects,
			p.dpu,
			mf.panel_addrs,
			mf.panel_map,
			da.trend_json
		FROM product_agg p
		LEFT JOIN map_final mf ON %s
		LEFT JOIN dpu_agg da ON %s
		ORDER BY p.process_code
	`,
		cteCols, groupByCols,
		cteCols,
		cteCols, cteCols, groupByCols, groupByCols,
		cteCols, groupByCols,
		cteCols, groupByCols,
		aliasedSelect, joinCond, dpuJoinCond)

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	fmt.Println("\n=== RESULTS ===")
	for rows.Next() {
		var proc, line, mach, path string
		var prod, defect int
		var dpu float64
		var addrs, pmap, trend sql.NullString

		if err := rows.Scan(&proc, &line, &mach, &path, &prod, &defect, &dpu, &addrs, &pmap, &trend); err != nil {
			t.Errorf("Scan error: %v", err)
			continue
		}

		fmt.Printf("[%s-%s] Prod:%d Def:%d DPU:%.2f Addrs:%v Trend:%v\n",
			proc, line, prod, defect, dpu, addrs.String, trend.String)
	}
}
