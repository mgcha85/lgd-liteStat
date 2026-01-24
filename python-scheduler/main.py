import os
import time
import schedule
import pandas as pd
import duckdb
import psycopg2
from dotenv import load_dotenv
import logging

# Setup Logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("scheduler")

# Load Environment Variables
load_dotenv()

SOURCE_DB_HOST = os.getenv("SOURCE_DB_HOST")
SOURCE_DB_PORT = os.getenv("SOURCE_DB_PORT", "5432")
SOURCE_DB_NAME = os.getenv("SOURCE_DB_NAME")
SOURCE_DB_USER = os.getenv("SOURCE_DB_USER")
SOURCE_DB_PASSWORD = os.getenv("SOURCE_DB_PASSWORD")
DUCKDB_PATH = os.getenv("DUCKDB_PATH", "/app/data/analytics.duckdb")
FACILITY_CODE = os.getenv("FACILITY_CODE", "default")
SCHEDULE_TIME = os.getenv("SCHEDULE_TIME", "03:00")

def connect_source_db():
    try:
        conn = psycopg2.connect(
            host=SOURCE_DB_HOST,
            port=SOURCE_DB_PORT,
            database=SOURCE_DB_NAME,
            user=SOURCE_DB_USER,
            password=SOURCE_DB_PASSWORD
        )
        return conn
    except Exception as e:
        logger.error(f"Failed to connect to source DB: {e}")
        return None

def download_data():
    logger.info("Starting data download job...")
    
    # 1. Connect to Source
    conn = connect_source_db()
    if not conn:
        logger.error("Skipping job due to connection failure.")
        return

    try:
        # Example Query - Customize based on actual schema
        query = "SELECT * FROM production_history WHERE created_at >= NOW() - INTERVAL '1 day'"
        
        logger.info("Executing query...")
        df = pd.read_sql(query, conn)
        logger.info(f"Downloaded {len(df)} rows.")

        if not df.empty:
            # 2. Save to DuckDB
            logger.info(f"Saving to DuckDB: {DUCKDB_PATH}")
            duck_conn = duckdb.connect(DUCKDB_PATH)
            
            # Use append or replace logic
            # Here we register the df as a view and insert
            duck_conn.register('temp_df', df)
            
            # Ensure table exists
            # duck_conn.execute("CREATE TABLE IF NOT EXISTS history AS SELECT * FROM temp_df WHERE 1=0") 
            # duck_conn.execute("INSERT INTO history SELECT * FROM temp_df")
            
            # For now, just print logic as schema might vary
            logger.info("Data saved successfully (Simulation).")
            
            duck_conn.close()
        else:
            logger.info("No new data found.")

    except Exception as e:
        logger.error(f"Error during data download: {e}")
    finally:
        conn.close()
    
    logger.info("Job finished.")

def run_scheduler():
    logger.info(f"Scheduler started. Task scheduled at {SCHEDULE_TIME} daily.")
    
    # Schedule the job
    schedule.every().day.at(SCHEDULE_TIME).do(download_data)
    
    # Run once on startup for verification (optional)
    # download_data()

    while True:
        schedule.run_pending()
        time.sleep(60)

if __name__ == "__main__":
    logger.info(f"Python Scheduler for Facility: {FACILITY_CODE}")
    run_scheduler()
