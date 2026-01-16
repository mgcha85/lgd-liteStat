#!/bin/bash

BASE_URL="http://localhost:8081/api"

echo "Testing API Endpoints..."

echo -n "1. Health Check: "
curl -s "$BASE_URL/health" | jq -r '.status'

echo -n "2. Get Config: "
curl -s "$BASE_URL/config" | jq -r '.Settings.Facilities[0]'

echo -n "3. Get Scheduler Config: "
curl -s "$BASE_URL/config/scheduler" | jq .

echo -n "4. Trigger Ingest (Incremental): "
curl -s -X POST "$BASE_URL/ingest" -H "Content-Type: application/json" | jq .

echo -n "5. Refresh Mart: "
curl -s -X POST "$BASE_URL/mart/refresh" -H "Content-Type: application/json" | jq -r '.status'

echo -n "6. Request Analysis (Wait for completion): "
# Start Job
JOB_ID=$(curl -s -X POST "$BASE_URL/analyze" -H "Content-Type: application/json" -d '{"defect_name":"SPOT-DARK","facility_code":"default","start_date":"2023-01-01","end_date":"2023-12-31","model_codes":["ModelA"],"process_codes":["P100"]}' | jq -r '.job_id')
echo "Job ID: $JOB_ID"

# Loop until complete
STATUS="pending"
while [ "$STATUS" != "completed" ]; do
    sleep 1
    STATUS=$(curl -s "$BASE_URL/analyze/$JOB_ID/status" | jq -r '.status')
    echo -n "."
done
echo " Done!"

echo -n "7. Get Analysis Results (with Images): "
# Check if images field exists
IMG_CHECK=$(curl -s "$BASE_URL/analyze/$JOB_ID/results?include_images=true" | jq '.images.daily_trend != null')
echo "Has Image: $IMG_CHECK"

echo -n "8. Get Equipment Rankings: "
curl -s "$BASE_URL/equipment/rankings?facility=default&start_date=2023-01-01" | jq '.count'

echo "API Test Complete."
