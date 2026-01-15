-- DuckDB Tables (Analytics)
CREATE TABLE IF NOT EXISTS inspection (
    glass_id TEXT,
    panel_id TEXT,
    product_id TEXT,
    panel_addr TEXT,
    term_name TEXT,
    defect_name TEXT,
    inspection_end_ymdhms TIMESTAMP,
    process_code TEXT,
    defect_count INTEGER
);

CREATE TABLE IF NOT EXISTS history (
    glass_id TEXT,
    product_id TEXT,
    lot_id TEXT,
    equipment_line_id TEXT,
    process_code TEXT,
    timekey_ymdhms TIMESTAMP,
    seq_num INTEGER
);

CREATE TABLE IF NOT EXISTS glass_stats (
    glass_id TEXT PRIMARY KEY,
    lot_id TEXT,
    product_id TEXT,
    work_date DATE,
    total_defects INTEGER,
    created_at TIMESTAMP
);

-- Operational Tables (Now in SQLite, schema kept here for reference but run separately)
-- The Go code will handle splitting this schema execution
