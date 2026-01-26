import argparse
import os
import yaml
import logging
from datetime import datetime, timedelta
from dotenv import load_dotenv
import duckdb

# Setup Logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger("update_defect_master")

load_dotenv()

# Env
DATA_DIR = os.getenv("DATA_DIR", "/app/data/lake")
DB_PATH = os.getenv("DB_PATH", "/app/data/analytics.duckdb")
CONFIG_PATH = os.getenv("CONFIG_PATH", "config.yaml")


def load_app_config():
    try:
        with open(CONFIG_PATH, "r") as f:
            return yaml.safe_load(f)
    except Exception as e:
        logger.error(f"Failed to load config.yaml: {e}")
        return None


def main():
    parser = argparse.ArgumentParser(description="Update Defect Master Table")
    parser.add_argument(
        "--start", type=str, required=True, help="Start date YYYY-MM-DD"
    )
    parser.add_argument("--end", type=str, required=True, help="End date YYYY-MM-DD")
    parser.add_argument("--fac", type=str, required=True, help="Facility Code")

    args = parser.parse_args()

    start_date = datetime.strptime(args.start, "%Y-%m-%d")
    end_date = datetime.strptime(args.end, "%Y-%m-%d")
    facility = args.fac

    con = duckdb.connect(DB_PATH)
    insp_root = os.path.join(DATA_DIR, "inspection")

    try:
        # 1. Ensure Table Exists
        con.execute("""
            CREATE TABLE IF NOT EXISTS defect_master (
                defect_name TEXT PRIMARY KEY,
                update_ymdhms TIMESTAMP
            )
        """)

        curr = start_date
        while curr <= end_date:
            target_day = curr.strftime("%Y-%m-%d")
            logger.info(
                f"Updating Defect Master for {target_day} (Facility: {facility})..."
            )

            # Check if Parquet file exists for this day/facility
            # Path: inspection/facility_code=.../year=.../month=.../inspection_YYYY-MM-DD.parquet
            # Using Hive partitioning detection via DuckDB is easiest

            # Optimization: Directly query the specific partition to avoid scanning everything
            # But read_parquet with hive_partitioning=true handles discovery.
            # To be safe and efficient, we can target the specific day's data if possible,
            # but our partitioning is by Year/Month.
            # We can filter by the file content or path if needed, but since we want to find UNIQUE defects
            # for that day, we can just query the glob pattern for that facility?
            # Actually, read_parquet is smart enough with WHERE clause on hive columns?
            # No, hive columns are year/month. The specific file is DATE.parquet.

            # Let's perform a read on the specific year/month folder and filter by file name pattern or just read everything and filter by date column if it exists.
            # However, simpler is to trust standard SQL filtering.

            # Read all inspection data for the facility
            # Filter by specific date (assuming inspection_end_ymdhms or similar exists?
            # Wait, `get_insp_data.py` uses `inspection_end_ymdhms` for query range).

            # Let's try to query the "Data Lake" via the View if it exists, or direct parquet read.
            # The view `inspection` exists in `schema_duckdb.sql` pointing to `/app/data/lake/inspection/**/*.parquet`.

            # Query:
            # SELECT DISTINCT defect_name FROM read_parquet(...) WHERE facility_code=... AND ...

            # DuckDB Upsert:
            # INSERT INTO defect_master (defect_name, update_ymdhms)
            # SELECT distinct defect_name, CURRENT_TIMESTAMP
            # FROM ...
            # ON CONFLICT (defect_name) DO UPDATE SET update_ymdhms = CURRENT_TIMESTAMP

            # We must handle 'defect_name' being NULL (from preprocess)

            upsert_sql = f"""
                INSERT INTO defect_master (defect_name, update_ymdhms)
                WITH daily_defects AS (
                    SELECT DISTINCT defect_name 
                    FROM read_parquet('{insp_root}/**/*.parquet', hive_partitioning=true)
                    WHERE facility_code = '{facility}'
                      -- AND strftime(inspection_end_ymdhms, '%Y-%m-%d') = '{target_day}' 
                      -- Use the filename-generated date column if possible? 
                      -- or just rely on the content. content is safer.
                      AND strftime(CAST(inspection_end_ymdhms AS DATE), '%Y-%m-%d') = '{target_day}'
                      AND defect_name IS NOT NULL
                )
                SELECT defect_name, CURRENT_TIMESTAMP
                FROM daily_defects
                ON CONFLICT (defect_name) DO UPDATE SET update_ymdhms = CURRENT_TIMESTAMP
            """

            try:
                con.execute(upsert_sql)
                logger.info(f"Updated defect_master for {target_day}.")
            except Exception as e:
                logger.warning(
                    f"Failed to update for {target_day} (Maybe no data?): {e}"
                )

            curr += timedelta(days=1)

    except Exception as e:
        logger.error(f"DuckDB Error: {e}")
    finally:
        con.close()


if __name__ == "__main__":
    main()
