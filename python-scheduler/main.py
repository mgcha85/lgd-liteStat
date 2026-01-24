import os
import time
import schedule
import pandas as pd
import yaml
from pathlib import Path
from datetime import datetime, timedelta
from dotenv import load_dotenv
import logging

# Import Custom DynamoDB Utils
from dynamodb import DynamoDB, load_column_info, build_expressions, items2df

# Setup Logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger("scheduler")

# Load Environment Variables
load_dotenv()

# Env Configs
AWS_ACCESS_KEY_ID = os.getenv("AWS_ACCESS_KEY_ID")
AWS_SECRET_ACCESS_KEY = os.getenv("AWS_SECRET_ACCESS_KEY")
AWS_REGION = os.getenv("AWS_REGION", "ap-northeast-2")
DYNAMODB_ENDPOINT = os.getenv(
    "DYNAMODB_ENDPOINT", "http://dynamodb.api.lgddna.kr/dynamodb"
)

DATA_DIR = os.getenv("DATA_DIR", "/app/data/lake")
CONFIG_PATH = os.getenv("CONFIG_PATH", "config.yaml")  # Service mounted config
SCHEDULE_TIME = os.getenv("SCHEDULE_TIME", "03:00")


def load_app_config():
    try:
        with open(CONFIG_PATH, "r") as f:
            return yaml.safe_load(f)
    except Exception as e:
        logger.error(f"Failed to load config.yaml: {e}")
        return None


def get_date_range(start_date_str, end_date_str):
    start = datetime.strptime(start_date_str, "%Y-%m-%d")
    end = datetime.strptime(end_date_str, "%Y-%m-%d")
    step = timedelta(days=1)
    while start <= end:
        yield start
        start += step


def save_to_parquet(df, table_type, facility, target_date):
    if df.empty:
        return

    year = target_date.year
    month = target_date.month
    day_str = target_date.strftime("%Y%m%d")

    # Hive Partition: facility_code=.../year=.../month=...
    partition_path = (
        Path(DATA_DIR)
        / table_type
        / f"facility_code={facility}"
        / f"year={year}"
        / f"month={month}"
    )
    partition_path.mkdir(parents=True, exist_ok=True)

    file_name = f"{table_type}_data_{day_str}.parquet"
    full_path = partition_path / file_name

    try:
        # Use pyarrow engine
        df.to_parquet(full_path, index=False, engine="pyarrow")
        logger.info(f"Saved {len(df)} rows to {full_path}")
    except Exception as e:
        logger.error(f"Failed to save parquet: {e}")


def run_download_job():
    logger.info("Starting Download Job...")
    config = load_app_config()
    if not config:
        logger.error("Configuration missing. Aborting.")
        return

    # Initialize DynamoDB Client
    dynamodb = DynamoDB(
        access_key=AWS_ACCESS_KEY_ID,
        secret_key=AWS_SECRET_ACCESS_KEY,
        endpoint=DYNAMODB_ENDPOINT,
        region=AWS_REGION,
    )

    # Global Settings
    facilities = config.get("settings", {}).get("facilities", [])
    ingest_cfg = config.get("ingest", {})

    # Define time range (e.g. Yesterday only, or range from config/args)
    # For scheduler, usually we download 'Yesterday's Data' or 'Fixed Range'
    # Implementation: Yesterday
    yesterday = datetime.now() - timedelta(days=1)
    target_dates = [yesterday]

    # If backfill needed, logic can be extended here

    # Iterate Facilities
    for fac in facilities:
        logger.info(f"Processing Facility: {fac}")

        for date in target_dates:
            date_str_YMD = date.strftime("%Y%m%d")
            # DynamoDB Condition: time_ymd > ... or between
            # Assuming schema has 'scan_time' or similar.
            # User example: time_ymhdhms > 20260101
            # We filter by day to minimize scan or query

            # --- 1. History Data ---
            if ingest_cfg.get("history_table"):
                table_name = ingest_cfg["history_table"]
                columns = ingest_cfg.get("history_columns", [])

                # Build Conditions
                # Adjust column names to match DB Schema (user snippet said 'time_ymhdhms')
                conditions = {
                    "facility_code": f"= '{fac}'",
                    "time_ymdhms": f"begins_with '{date.strftime('%Y-%m-%d')}'",  # Example
                }

                logger.info(f"Fetching History for {fac} on {date.date()}")
                try:
                    schema = load_column_info(table_name=table_name)
                    exprs = build_expressions(table_name, schema, columns, conditions)

                    # Execute Query
                    response = dynamodb.query(**exprs)
                    df = items2df(response.get("Items"), schema, columns, conditions)

                    save_to_parquet(df, "history", fac, date)

                except Exception as e:
                    logger.error(f"Error processing history for {fac}: {e}")

            # --- 2. Inspection Data ---
            if ingest_cfg.get("inspection_table"):
                table_name = ingest_cfg["inspection_table"]
                columns = ingest_cfg.get("inspection_columns", [])

                conditions = {
                    "facility_code": f"= '{fac}'",
                    "inspection_end_ymdhms": f"begins_with '{date.strftime('%Y-%m-%d')}'",
                }

                logger.info(f"Fetching Inspection for {fac} on {date.date()}")
                try:
                    schema = load_column_info(table_name=table_name)
                    exprs = build_expressions(table_name, schema, columns, conditions)

                    response = dynamodb.query(**exprs)
                    df = items2df(response.get("Items"), schema, columns, conditions)

                    save_to_parquet(df, "inspection", fac, date)

                except Exception as e:
                    logger.error(f"Error processing inspection for {fac}: {e}")

    logger.info("Download Job Completed.")


def run_scheduler():
    logger.info(f"Scheduler started. Task scheduled at {SCHEDULE_TIME} daily.")
    schedule.every().day.at(SCHEDULE_TIME).do(run_download_job)

    # Run loop
    while True:
        schedule.run_pending()
        time.sleep(60)


if __name__ == "__main__":
    # Optional: Run immediately if env flag set?
    # For now just start scheduler.
    # To test immediately: uncomment next line
    # run_download_job()

    run_scheduler()
