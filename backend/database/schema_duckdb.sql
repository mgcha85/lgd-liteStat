-- DuckDB Tables (Analytics)

-- Views pointing to Parquet Data Lake
-- Note: Views are generally safe to replace (CREATE OR REPLACE) but tables must NEVER be dropped in this script.
-- cleanup section removed to prevent data loss.

-- Mart Table (Still needed for aggregation speed)
CREATE TABLE IF NOT EXISTS glass_stats (
    product_id TEXT,
    defect_name TEXT,
    model_code TEXT,
    lot_id TEXT,
    work_date DATE,
    total_defects INTEGER,
    panel_map INTEGER[],
    panel_addrs TEXT[],
    created_at TIMESTAMP,
    PRIMARY KEY (product_id, defect_name)
);

-- Ensure columns exist (Migration for existing tables)
ALTER TABLE glass_stats ADD COLUMN IF NOT EXISTS model_code TEXT;
ALTER TABLE glass_stats ADD COLUMN IF NOT EXISTS defect_name TEXT;
ALTER TABLE glass_stats ADD COLUMN IF NOT EXISTS panel_map INTEGER[];
ALTER TABLE glass_stats ADD COLUMN IF NOT EXISTS panel_addrs TEXT[];

-- VIEWS pointing to Parquet Data Lake
-- Note: hive_partitioning=1 enables auto-discovery of facility_code, year, month columns from path
-- union_by_name=True allows schema evolution if new columns are added later
CREATE OR REPLACE VIEW inspection AS 
SELECT * 
FROM read_parquet('/app/data/lake/inspection/**/*.parquet', hive_partitioning=1, union_by_name=True);

CREATE OR REPLACE VIEW history AS 
SELECT * 
FROM read_parquet('/app/data/lake/history/**/*.parquet', hive_partitioning=1, union_by_name=True);
