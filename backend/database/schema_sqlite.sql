-- SQLite Tables (Operations)

CREATE TABLE IF NOT EXISTS analysis_cache (
    cache_key TEXT PRIMARY KEY,
    request_params TEXT,   -- JSON stored as TEXT
    glass_results TEXT,    -- JSON stored as TEXT
    lot_results TEXT,      -- JSON stored as TEXT
    daily_results TEXT,    -- JSON stored as TEXT
    heatmap_results TEXT,  -- JSON stored as TEXT
    metrics TEXT,          -- JSON stored as TEXT
    batch_results TEXT,    -- NEW: JSON map of AnalysisResults for Batch
    created_at DATETIME,
    expires_at DATETIME
);

CREATE TABLE IF NOT EXISTS analysis_jobs (
    job_id TEXT PRIMARY KEY,
    status TEXT,
    cache_key TEXT,
    error_message TEXT,
    progress INTEGER,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS analysis_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_time DATETIME DEFAULT CURRENT_TIMESTAMP,
    defect_name TEXT,
    start_date TEXT,
    end_date TEXT,
    target_count INTEGER,
    glass_count INTEGER,
    duration_ms INTEGER,
    status TEXT
);
