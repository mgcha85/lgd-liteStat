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

-- VIEWS pointing to Parquet Data Lake
-- Note: hive_partitioning=1 enables auto-discovery of facility_code, year, month columns from path
-- union_by_name=True allows schema evolution if new columns are added later
CREATE OR REPLACE VIEW inspection AS 
SELECT * 
FROM read_parquet('/app/data/lake/inspection/**/*.parquet', hive_partitioning=1, union_by_name=True);

CREATE OR REPLACE VIEW history AS 
SELECT * 
FROM read_parquet('/app/data/lake/history/**/*.parquet', hive_partitioning=1, union_by_name=True);
