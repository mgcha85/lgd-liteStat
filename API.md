# API Reference - LGD liteStat

Complete API documentation with curl examples for the display manufacturing data analysis system.

## Base URL

```
http://localhost:8080
```

---

## 📋 Table of Contents

1. [Health Check](#1-health-check)
2. [Data Query APIs](#data-query-apis)
   - [Query Inspection Data](#21-query-inspection-data)
   - [Query History Data](#22-query-history-data)
3. [Data Management APIs](#data-management-apis)
   - [Ingest Data](#31-ingest-data)
   - [Refresh Data Mart](#32-refresh-data-mart)
   - [Cleanup Old Data](#33-cleanup-old-data)
4. [Analysis APIs](#analysis-apis)
   - [Request Analysis](#41-request-analysis)
   - [Check Analysis Status](#42-check-analysis-status)
   - [Get Analysis Results](#43-get-analysis-results)
   - [Get Equipment Rankings](#44-get-equipment-rankings)

---

## 1. Health Check

Check API and database health status.

### Endpoint
```
GET /health
GET /api/health
```

### Request
```bash
curl http://localhost:8080/health
```

### Response (200 OK)
```json
{
  "status": "healthy",
  "stats": {
    "inspection": 1000000,
    "history": 500000,
    "glass_stats": 166666,
    "analysis_cache": 5,
    "analysis_jobs": 10
  }
}
```

---

## Data Query APIs

### 2.1 Query Inspection Data

Query inspection data by time range with optional filters.

#### Endpoint
```
GET /api/inspection
```

#### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| start_time | string | **Yes** | Start time (format: `YYYY-MM-DD HH:MM:SS`) |
| end_time | string | **Yes** | End time (format: `YYYY-MM-DD HH:MM:SS`) |
| process_code | string | No | Filter by process code (e.g., `P100`) |
| defect_name | string | No | Filter by defect name (e.g., `SPOT-DARK`) |
| limit | integer | No | Max records to return (default: 1000) |
| offset | integer | No | Offset for pagination (default: 0) |

#### Request Examples

**Basic query by time range:**
```bash
curl "http://localhost:8080/api/inspection?start_time=2024-01-01%2000:00:00&end_time=2024-01-31%2023:59:59"
```

**With process code filter:**
```bash
curl "http://localhost:8080/api/inspection?start_time=2024-01-01%2000:00:00&end_time=2024-01-31%2023:59:59&process_code=P100"
```

**With defect name filter:**
```bash
curl "http://localhost:8080/api/inspection?start_time=2024-01-01%2000:00:00&end_time=2024-01-31%2023:59:59&defect_name=SPOT-DARK"
```

**With pagination:**
```bash
curl "http://localhost:8080/api/inspection?start_time=2024-01-01%2000:00:00&end_time=2024-01-31%2023:59:59&limit=100&offset=0"
```

**All filters combined:**
```bash
curl "http://localhost:8080/api/inspection?start_time=2024-01-01%2000:00:00&end_time=2024-01-31%2023:59:59&process_code=P100&defect_name=SPOT-DARK&limit=100&offset=0"
```

#### Response (200 OK)
```json
{
  "data": [
    {
      "glass_id": "G00000001",
      "panel_id": "ABCDEFAB1",
      "product_id": "ABCDEF",
      "panel_addr": "AB1",
      "term_name": "TYPE1-SPOT-SIZE-DARK",
      "defect_name": "SPOT-DARK",
      "inspection_end_ymdhms": "2024-01-15T14:30:25Z",
      "process_code": "P100",
      "defect_count": 2
    }
  ],
  "pagination": {
    "limit": 100,
    "offset": 0,
    "total_count": 15234,
    "has_more": true
  }
}
```

---

### 2.2 Query History Data

Query glass progression history by glass_id with optional filters.

#### Endpoint
```
GET /api/history
```

#### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| glass_id | string | **Yes** | Glass ID to query (e.g., `G00000001`) |
| process_code | string | No | Filter by process code |
| equipment_id | string | No | Filter by equipment ID |

#### Request Examples

**Query by glass_id:**
```bash
curl "http://localhost:8080/api/history?glass_id=G00000001"
```

**With process code filter:**
```bash
curl "http://localhost:8080/api/history?glass_id=G00000001&process_code=P100"
```

**With equipment filter:**
```bash
curl "http://localhost:8080/api/history?glass_id=G00000001&equipment_id=EQ001"
```

**All filters combined:**
```bash
curl "http://localhost:8080/api/history?glass_id=G00000001&process_code=P100&equipment_id=EQ001"
```

#### Response (200 OK)
```json
{
  "glass_id": "G00000001",
  "data": [
    {
      "glass_id": "G00000001",
      "product_id": "ABCDEF",
      "lot_id": "LOT000001",
      "equipment_line_id": "EQ001",
      "process_code": "P100",
      "timekey_ymdhms": "2024-01-15T10:00:00Z",
      "seq_num": 1
    },
    {
      "glass_id": "G00000001",
      "product_id": "ABCDEF",
      "lot_id": "LOT000001",
      "equipment_line_id": "EQ002",
      "process_code": "P200",
      "timekey_ymdhms": "2024-01-15T12:00:00Z",
      "seq_num": 1
    }
  ],
  "count": 2
}
```

---

## Data Management APIs

### 3.1 Ingest Data

Download and ingest data from source system (or generate mock data).

#### Endpoint
```
POST /api/ingest
```

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| start_time | string | **Yes** | Start time (RFC3339 format) |
| end_time | string | **Yes** | End time (RFC3339 format) |

#### Request Example
```bash
curl -X POST http://localhost:8080/api/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "start_time": "2024-01-01T00:00:00Z",
    "end_time": "2024-01-01T23:59:59Z"
  }'
```

#### Response (200 OK)
```json
{
  "status": "success",
  "records_inserted": {
    "inspection": 25000,
    "history": 12000
  }
}
```

#### Crontab Example (Hourly)
```bash
# Download last hour of data every hour
0 * * * * curl -X POST http://localhost:8080/api/ingest -H "Content-Type: application/json" -d '{"start_time":"'$(date -u -d '1 hour ago' +\%Y-\%m-\%dT\%H:00:00Z)'","end_time":"'$(date -u +\%Y-\%m-\%dT\%H:00:00Z)'"}'
```

---

### 3.2 Refresh Data Mart

Rebuild the glass_stats materialized view for optimized analysis queries.

#### Endpoint
```
POST /api/mart/refresh
```

#### Request Example
```bash
curl -X POST http://localhost:8080/api/mart/refresh
```

#### Response (200 OK)
```json
{
  "status": "success",
  "duration_ms": 2347,
  "rows_created": 166666,
  "stats": {
    "total_rows": 166666,
    "min_date": "2024-01-01",
    "max_date": "2025-02-05",
    "avg_defects_per_glass": 1.5,
    "total_defects": 1000000,
    "unique_lots": 5555
  }
}
```

#### Crontab Example (Every hour, 5 minutes after ingestion)
```bash
5 * * * * curl -X POST http://localhost:8080/api/mart/refresh
```

---

### 3.3 Cleanup Old Data

Delete data older than the configured retention period (default: 1 year).

#### Endpoint
```
POST /api/cleanup
```

#### Request Example
```bash
curl -X POST http://localhost:8080/api/cleanup
```

#### Response (200 OK)
```json
{
  "status": "success",
  "deleted_rows": {
    "inspection": 50000,
    "history": 25000,
    "glass_stats": 8333,
    "analysis_cache": 2,
    "analysis_jobs": 5
  }
}
```

#### Crontab Example (Daily at 2 AM)
```bash
0 2 * * * curl -X POST http://localhost:8080/api/cleanup
```

---

## Analysis APIs

### 4.1 Request Analysis

Submit an asynchronous analysis job for Target vs Others comparison.

#### Endpoint
```
POST /api/analyze
```

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| defect_name | string | **Yes** | Defect name to analyze (e.g., `SPOT-DARK`) |
| start_date | string | **Yes** | Start date (format: `YYYY-MM-DD`) |
| end_date | string | **Yes** | End date (format: `YYYY-MM-DD`) |
| process_codes | array[string] | No | Filter by process codes (e.g., `["P100","P200"]`) |
| equipment_ids | array[string] | No | Equipment IDs for Target group (required for meaningful analysis) |

#### Request Example
```bash
curl -X POST http://localhost:8080/api/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "defect_name": "SPOT-DARK",
    "start_date": "2024-01-01",
    "end_date": "2024-12-31",
    "process_codes": ["P100", "P200"],
    "equipment_ids": ["EQ001"]
  }'
```

#### Response (202 Accepted)
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending"
}
```

**Note:** Save the `job_id` to check status and retrieve results.

---

### 4.2 Check Analysis Status

Check the status of an analysis job.

#### Endpoint
```
GET /api/analyze/{jobId}/status
```

#### Request Example
```bash
JOB_ID="550e8400-e29b-41d4-a716-446655440000"
curl "http://localhost:8080/api/analyze/${JOB_ID}/status"
```

#### Response - Pending (200 OK)
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "progress": 0,
  "created_at": "2024-01-15T05:30:00Z",
  "updated_at": "2024-01-15T05:30:00Z"
}
```

#### Response - Running (200 OK)
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "running",
  "progress": 50,
  "created_at": "2024-01-15T05:30:00Z",
  "updated_at": "2024-01-15T05:30:05Z"
}
```

#### Response - Completed (200 OK)
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "cache_key": "a3f2b8c9d1e6...",
  "progress": 100,
  "created_at": "2024-01-15T05:30:00Z",
  "updated_at": "2024-01-15T05:30:10Z"
}
```

#### Response - Failed (200 OK)
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "failed",
  "error_message": "Equipment filter returned no glasses",
  "progress": 25,
  "created_at": "2024-01-15T05:30:00Z",
  "updated_at": "2024-01-15T05:30:03Z"
}
```

#### Polling Pattern
```bash
# Wait for job completion
while true; do
  STATUS=$(curl -s "http://localhost:8080/api/analyze/${JOB_ID}/status" | jq -r '.status')
  echo "Status: $STATUS"
  [[ "$STATUS" == "completed" ]] && break
  [[ "$STATUS" == "failed" ]] && exit 1
  sleep 2
done
```

---

### 4.3 Get Analysis Results

Retrieve the results of a completed analysis job. Returns 4 result sets:
1. **Glass-level** (scatter plot data)
2. **Lot-level** (aggregated by lot)
3. **Daily** (time series)
4. **Heatmap** (panel position distribution)

Plus summary **metrics**.

#### Endpoint
```
GET /api/analyze/{jobId}/results
```

#### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| limit | integer | 100 | Max glass results to return |
| offset | integer | 0 | Offset for glass results pagination |

#### Request Examples

**Basic request:**
```bash
JOB_ID="550e8400-e29b-41d4-a716-446655440000"
curl "http://localhost:8080/api/analyze/${JOB_ID}/results"
```

---



**With pagination:**
```bash
curl "http://localhost:8080/api/analyze/${JOB_ID}/results?limit=100&offset=0"
```

**Pretty-printed with jq:**
```bash
curl -s "http://localhost:8080/api/analyze/${JOB_ID}/results?limit=10" | jq .
```

#### Response (200 OK)
```json
{
  "glass_results": [
    {
      "glass_id": "G00000001",
      "lot_id": "LOT000001",
      "work_date": "2024-01-15",
      "total_defects": 3,
      "group_type": "Target"
    },
    {
      "glass_id": "G00000002",
      "lot_id": "LOT000001",
      "work_date": "2024-01-15",
      "total_defects": 1,
      "group_type": "Others"
    }
  ],
  "lot_results": [
    {
      "lot_id": "LOT000001",
      "group_type": "Target",
      "glass_count": 10,
      "total_defects": 28,
      "avg_defects": 2.8,
      "max_defects": 5
    },
    {
      "lot_id": "LOT000001",
      "group_type": "Others",
      "glass_count": 20,
      "total_defects": 30,
      "avg_defects": 1.5,
      "max_defects": 4
    }
  ],
  "daily_results": [
    {
      "work_date": "2024-01-15",
      "group_type": "Target",
      "glass_count": 120,
      "total_defects": 336,
      "avg_defects": 2.8
    },
    {
      "work_date": "2024-01-15",
      "group_type": "Others",
      "glass_count": 4880,
      "total_defects": 7320,
      "avg_defects": 1.5
    }
  ],
  "heatmap_results": [
    {
      "x": "AB",
      "y": "1",
      "defect_rate": 2.5,
      "total_defects": 50,
      "total_glasses": 20
    }
  ],
  "metrics": {
    "overall_defect_rate": 1.6,
    "target_defect_rate": 2.8,
    "others_defect_rate": 1.5,
    "delta": -1.2,
    "superiority_indicator": -1.3,
    "target_glass_count": 120,
    "others_glass_count": 4880
  },
  "pagination": {
    "limit": 100,
    "offset": 0,
    "total_count": 5000,
    "has_more": true
  },
  "created_at": "2024-01-15T05:30:10Z"
}
```

**Metrics Explanation:**
- `overall_defect_rate`: Average defects across all glasses
- `target_defect_rate`: Average defects for Target group (glasses through selected equipment)
- `others_defect_rate`: Average defects for Others group
- `delta`: `overall_defect_rate - target_defect_rate` (negative if target is worse)
- `superiority_indicator`: `others_defect_rate - target_defect_rate` (positive if target is better)

---

### 4.4 Export Analysis Images [New]

Export generated chart images (Daily Trend, Heatmap) for a specific equipment in an analysis job. Returns a ZIP file.

#### Endpoint
```
GET /api/analyze/{jobId}/images
```

#### Query Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| equipment_id | string | **Yes** | Equipment ID to generate charts for |

#### Request Example

```bash
curl -o charts.zip "http://localhost:8080/api/analyze/${JOB_ID}/images?equipment_id=EQ001"
```

#### Response (200 OK)
- **Content-Type**: `application/zip`
- **Body**: ZIP file containing `daily_trend.png`, `heatmap.svg`, etc.

---

### 4.5 Get Equipment Rankings

Get top equipments ranked by defect rate delta (overall rate - equipment rate). Higher delta means equipment causes fewer defects than average.

#### Endpoint
```
GET /api/equipment/rankings
```

#### Query Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| start_date | string | **Yes** | Start date (format: `YYYY-MM-DD`) |
| end_date | string | **Yes** | End date (format: `YYYY-MM-DD`) |
| defect_name | string | No | Filter by defect name |
| limit | integer | No | Max results (default: 100) |

#### Request Examples

**Basic query:**
```bash
curl "http://localhost:8080/api/equipment/rankings?start_date=2024-01-01&end_date=2024-12-31"
```

**With defect filter:**
```bash
curl "http://localhost:8080/api/equipment/rankings?start_date=2024-01-01&end_date=2024-12-31&defect_name=SPOT-DARK"
```

**With limit:**
```bash
curl "http://localhost:8080/api/equipment/rankings?start_date=2024-01-01&end_date=2024-12-31&limit=50"
```

#### Response (200 OK)
```json
{
  "rankings": [
    {
      "equipment_id": "EQ002",
      "process_code": "P200",
      "glass_count": 15000,
      "total_defects": 18000,
      "defect_rate": 1.2,
      "overall_rate": 1.5,
      "delta": 0.3
    },
    {
      "equipment_id": "EQ001",
      "process_code": "P100",
      "glass_count": 10000,
      "total_defects": 18000,
      "defect_rate": 1.8,
      "overall_rate": 1.5,
      "delta": -0.3
    }
  ],
  "count": 2
}
```

**Delta Interpretation:**
- **Positive delta**: Equipment performs better than average (good)
- **Negative delta**: Equipment performs worse than average (problematic)
- **Zero delta**: Equipment performs at average

---

## Error Responses

### 400 Bad Request
```json
{
  "error": "start_time and end_time are required (format: YYYY-MM-DD HH:MM:SS)"
}
```

### 404 Not Found
```json
{
  "error": "job not found"
}
```

### 409 Conflict
```json
{
  "error": "job is not completed yet"
}
```

### 500 Internal Server Error
```json
{
  "error": "query failed: database connection lost"
}
```

### 503 Service Unavailable
```json
{
  "error": "database health check failed"
}
```

---

## Complete Workflow Example

### Scenario: Daily Analysis Report

```bash
#!/bin/bash

# 1. Ingest yesterday's data
curl -X POST http://localhost:8080/api/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "start_time": "'$(date -u -d 'yesterday' +%Y-%m-%d)'T00:00:00Z",
    "end_time": "'$(date -u -d 'yesterday' +%Y-%m-%d)'T23:59:59Z"
  }'

# 2. Refresh mart
curl -X POST http://localhost:8080/api/mart/refresh

# 3. Get equipment rankings
curl -s "http://localhost:8080/api/equipment/rankings?start_date=$(date -d 'yesterday' +%Y-%m-%d)&end_date=$(date -d 'yesterday' +%Y-%m-%d)&defect_name=SPOT-DARK" \
  | jq '.rankings[] | select(.delta < 0)'  # Show problematic equipment

# 4. Analyze worst equipment
WORST_EQ=$(curl -s "..." | jq -r '.rankings[0].equipment_id')

JOB_ID=$(curl -s -X POST http://localhost:8080/api/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "defect_name": "SPOT-DARK",
    "start_date": "'$(date -d '30 days ago' +%Y-%m-%d)'",
    "end_date": "'$(date +%Y-%m-%d)'",
    "equipment_ids": ["'$WORST_EQ'"]
  }' | jq -r '.job_id')

# 5. Wait for completion
while true; do
  STATUS=$(curl -s "http://localhost:8080/api/analyze/${JOB_ID}/status" | jq -r '.status')
  [[ "$STATUS" == "completed" ]] && break
  sleep 2
done

# 6. Get results
curl -s "http://localhost:8080/api/analyze/${JOB_ID}/results" | jq '.metrics'
```

---

## Rate Limiting & Best Practices

1. **Analysis jobs**: Cached by request parameters. Identical requests return immediately.
2. **Pagination**: Use `limit` and `offset` for large result sets.
3. **Time range**: For inspection queries, limit to reasonable ranges (e.g., 1 month max) to avoid timeouts.
4. **Concurrent requests**: Worker pool handles up to 4 concurrent analysis jobs by default.

---

## Data Formats

### Date/Time Formats

- **RFC3339**: `2024-01-15T14:30:00Z` (for API requests)
- **Date only**: `2024-01-15` (for start_date/end_date)
- **SQL timestamp**: `2024-01-15 14:30:00` (for inspection/history queries)

### Defect Name Format

Extracted from `term_name` using elements 2 and 4:
- `term_name`: `"TYPE1-SPOT-SIZE-DARK"`
- `defect_name`: `"SPOT-DARK"`

### Panel Address Format

Calculated as `panel_id - product_id`:
- `panel_id`: `"ABCDEFAB1"`
- `product_id`: `"ABCDEF"`
- `panel_addr`: `"AB1"`
