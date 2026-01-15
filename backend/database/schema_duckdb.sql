-- DuckDB Tables (Analytics)
CREATE SCHEMA IF NOT EXISTS lake_mgr;

-- Drop tables to ensure schema update
DROP TABLE IF EXISTS lake_mgr.eas_pnl_ins_def_a;
DROP TABLE IF EXISTS lake_mgr.mas_pnl_prod_eqp_h;
DROP TABLE IF EXISTS glass_stats;

-- Inspection Table
CREATE TABLE IF NOT EXISTS lake_mgr.eas_pnl_ins_def_a (
    facility_code TEXT,
    inspection_end_ymdhms TIMESTAMP,
    defect_seq_no INTEGER,
    product_id TEXT, -- Unit ID (was glass_id)
    panel_id TEXT,
    process_code TEXT,
    process_term_name_s TEXT,
    process_group_code TEXT,
    lot_id TEXT,
    equipment_group_id TEXT,
    equipment_id TEXT,
    equipment_term_name_s TEXT,
    part_no_term_name TEXT,
    production_type_code TEXT,
    model_code TEXT,
    final_flag TEXT,
    def_latest_judgement_code TEXT,
    def_latest_summary_defect_term_name_s TEXT, -- Source Term Name
    def_pnt_x FLOAT,
    def_pnt_y FLOAT,
    def_pnt_g INTEGER,
    def_pnt_d INTEGER,
    def_size FLOAT,
    
    -- Derived Columns
    defect_name TEXT, -- Derived from def_latest_summary_defect_term_name_s (Parts 2+4)
    panel_addr TEXT,
    panel_x TEXT,
    panel_y TEXT
);

-- History Table
CREATE TABLE IF NOT EXISTS lake_mgr.mas_pnl_prod_eqp_h (
    process_code TEXT,
    move_in_ymdhms TIMESTAMP,
    equipment_id TEXT,
    data_insert_ymdhms TIMESTAMP,
    data_update_ymdhms TIMESTAMP,
    receive_ymdhms TIMESTAMP,
    etl_insert_update_ymdhms TIMESTAMP,
    factory_code TEXT,
    product_type_code TEXT,
    product_id TEXT, -- Was original_glass_id
    original_product_id TEXT,
    apd_seq_no INTEGER,
    apd_data_id TEXT,
    equipment_hierarchy_type_code TEXT,
    equipment_line_id TEXT,
    equipment_machine_id TEXT,
    equipment_unit_id TEXT,
    equipment_path_id TEXT,
    delete_flag TEXT,
    equip_timekey_ymdhms TIMESTAMP,
    pre_equipment_status_code TEXT,
    equipment_status_code TEXT,
    
    -- Synthesized/Critical Columns (Encourage keeping lot_id if possible, or derive)
    lot_id TEXT -- Kept for compatibility with existing Analysis logic
);

CREATE TABLE IF NOT EXISTS glass_stats (
    product_id TEXT PRIMARY KEY,
    lot_id TEXT,
    product_model_code TEXT, -- Was product_id
    work_date DATE,
    total_defects INTEGER,
    created_at TIMESTAMP
);
