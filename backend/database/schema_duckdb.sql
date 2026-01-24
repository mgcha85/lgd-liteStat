-- DuckDB Tables (Analytics)
CREATE SCHEMA IF NOT EXISTS lake_mgr;

-- Drop legacy tables/views
DROP TABLE IF EXISTS lake_mgr.eas_pnl_ins_def_a;
DROP TABLE IF EXISTS lake_mgr.mas_pnl_prod_eqp_h;
DROP TABLE IF EXISTS glass_stats;
DROP VIEW IF EXISTS inspection;
DROP VIEW IF EXISTS history;

-- Mart Table (Still needed for aggregation speed)
CREATE TABLE IF NOT EXISTS glass_stats (
    product_id TEXT PRIMARY KEY,
    lot_id TEXT,
    product_model_code TEXT, -- Was product_id
    work_date DATE,
    total_defects INTEGER,
    created_at TIMESTAMP
);

-- VIEWS pointing to Parquet Data Lake
-- Note: hive_partitioning=1 enables auto-discovery of facility_code, year, month columns from path
-- union_by_name=True allows schema evolution if new columns are added later
CREATE OR REPLACE VIEW inspection AS 
SELECT * 
FROM read_parquet('/app/data/lake/inspection/**/*.parquet', hive_partitioning=1, union_by_name=True);

CREATE OR REPLACE VIEW history AS 
SELECT * 
FROM read_parquet('/app/data/lake/history/**/*.parquet', hive_partitioning=1, union_by_name=True);
