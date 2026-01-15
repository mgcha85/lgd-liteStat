-- Display Manufacturing Data Analysis System - Database Schema
-- DuckDB columnar database optimized for OLAP queries

-- Raw inspection data from manufacturing process
CREATE TABLE IF NOT EXISTS inspection (
    glass_id TEXT NOT NULL,
    panel_id TEXT,
    product_id TEXT,
    panel_addr TEXT,  -- Computed: panel_id - product_id
    term_name TEXT,   -- Raw defect term (e.g., "TYPE1-SPOT-SIZE-DARK")
    defect_name TEXT, -- Computed: elements 2 and 4 from term_name (e.g., "SPOT-DARK")
    inspection_end_ymdhms TIMESTAMP NOT NULL,
    process_code TEXT,
    defect_count INTEGER DEFAULT 1
);

-- Create indexes for join performance
CREATE INDEX IF NOT EXISTS idx_inspection_glass_id ON inspection(glass_id);
CREATE INDEX IF NOT EXISTS idx_inspection_date ON inspection(inspection_end_ymdhms);
CREATE INDEX IF NOT EXISTS idx_inspection_defect ON inspection(defect_name);

-- Glass progression history through equipment
CREATE TABLE IF NOT EXISTS history (
    glass_id TEXT NOT NULL,
    product_id TEXT,
    lot_id TEXT,
    equipment_line_id TEXT,
    process_code TEXT,
    timekey_ymdhms TIMESTAMP NOT NULL,
    seq_num INTEGER DEFAULT 1  -- For duplicate handling (highest = latest)
);

-- Create indexes for join and filtering
CREATE INDEX IF NOT EXISTS idx_history_glass_id ON history(glass_id);
CREATE INDEX IF NOT EXISTS idx_history_lot_id ON history(lot_id);
CREATE INDEX IF NOT EXISTS idx_history_equipment ON history(equipment_line_id);
CREATE INDEX IF NOT EXISTS idx_history_date ON history(timekey_ymdhms);
CREATE INDEX IF NOT EXISTS idx_history_composite ON history(glass_id, process_code, equipment_line_id);

-- Pre-aggregated mart table (核心 - Core Performance Optimization)
-- This table is rebuilt periodically to accelerate queries
CREATE TABLE IF NOT EXISTS glass_stats (
    glass_id TEXT PRIMARY KEY,
    lot_id TEXT,
    product_id TEXT,
    work_date DATE,           -- For daily time series
    total_defects INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_glass_stats_lot ON glass_stats(lot_id);
CREATE INDEX IF NOT EXISTS idx_glass_stats_date ON glass_stats(work_date);
CREATE INDEX IF NOT EXISTS idx_glass_stats_product ON glass_stats(product_id);

-- Cached analysis results
CREATE TABLE IF NOT EXISTS analysis_cache (
    cache_key TEXT PRIMARY KEY,
    request_params JSON,
    glass_results JSON,     -- Glass-level scatter data
    lot_results JSON,       -- Lot-level aggregation
    daily_results JSON,     -- Daily time series
    heatmap_results JSON,   -- Panel position heatmap
    metrics JSON,           -- Summary metrics
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP
);

-- Create index for cleanup queries
CREATE INDEX IF NOT EXISTS idx_cache_expires ON analysis_cache(expires_at);

-- Async job tracking
CREATE TABLE IF NOT EXISTS analysis_jobs (
    job_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,  -- 'pending', 'running', 'completed', 'failed'
    cache_key TEXT,
    error_message TEXT,
    progress INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create index for status queries
CREATE INDEX IF NOT EXISTS idx_jobs_status ON analysis_jobs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_cache ON analysis_jobs(cache_key);
