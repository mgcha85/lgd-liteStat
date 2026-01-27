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
logger = logging.getLogger("run_daily_analysis")

load_dotenv()

# Env
DATA_DIR = os.getenv("DATA_DIR", "/app/data/lake")
CONFIG_PATH = os.getenv("CONFIG_PATH", "config.yaml")


def load_app_config():
    try:
        with open(CONFIG_PATH, "r") as f:
            return yaml.safe_load(f)
    except Exception as e:
        logger.error(f"Failed to load config.yaml: {e}")
        return None


def main():
    parser = argparse.ArgumentParser(description="Run Daily Analysis Batch")
    parser.add_argument(
        "--start", type=str, required=True, help="Start date YYYY-MM-DD"
    )
    parser.add_argument("--end", type=str, required=True, help="End date YYYY-MM-DD")
    parser.add_argument("--fac", type=str, required=True, help="Facility Code")

    args = parser.parse_args()

    start_date = datetime.strptime(args.start, "%Y-%m-%d")
    end_date = datetime.strptime(args.end, "%Y-%m-%d")
    facility = args.fac

    config = load_app_config()
    # Default to panel_addr if config/key missing
    agg_key = "panel_addr"
    if config and "analysis" in config:
        agg_key = config["analysis"].get("defect_aggregation_key", "panel_addr")

    # 1. DuckDB per Facility
    db_file = os.path.join(DATA_DIR, f"{facility}.duckdb")
    con = duckdb.connect(db_file)
    logger.info(f"Connected to DuckDB: {db_file}")

    history_root = os.path.join(DATA_DIR, "history")
    inspection_root = os.path.join(DATA_DIR, "inspection")

    try:
        # Create Table (Single Table Approach)
        # PK: product_id + defect_name (to separate analysis per defect type)
        # If no defect, defect_name will be NULL (or 'NORMAL' if we choose).
        # But PK cannot be NULL. So we should use a default like 'NONE' or rely on DuckDB handling (Composite PK with NULL is tricky).
        # Ideally, we store "Defect Info".
        # If history exists but no defect, we insert one row with defect_name='OK'?
        # Or Just allow NULL if strict PK isn't enforced or handle it.
        # Let's use 'NORMAL' or 'NO_DEFECT' for clean PKs if conflict handling is needed.

        con.execute("""
            CREATE TABLE IF NOT EXISTS glass_stats (
                product_id TEXT,
                defect_name TEXT,
                model_code TEXT,
                lot_id TEXT,
                work_date DATE,
                total_defects INTEGER,
                panel_addrs TEXT[],
                created_at TIMESTAMP,
                PRIMARY KEY (product_id, defect_name)
            )
        """)

        curr = start_date
        while curr <= end_date:
            # 2. Target Date = 2 weeks ago from current loop date?
            # User requirement: "Fetch 2 weeks ago data from today (criteria)"
            # If the batch runs daily for 'yesterday', let's assume 'curr' IS the 'today' the user refers to,
            # OR the user wants this script to always analyze T-14 days.
            # However, 'args.start' / 'args.end' implies a range.
            # If we are filling past data, we should treat 'curr' as the target analysis date.
            # But the requirement says "Fetch data of 2 weeks ago".
            # Let's interpret: We are analyzing data generated on (curr - 14 days).
            # This handles the "settling time" or "retrospective" aspect.
            # Wait, inspection data path is facility/year/month/data_{ymdhms}.parquet.
            # We need to filter this efficiently.

            target_date_str = curr.strftime("%Y-%m-%d")
            logger.info(
                f"Analyzing data for date: {target_date_str} (Batch Date: {curr.strftime('%Y-%m-%d')})"
            )

            # 3. Join Logic
            # Inspection Data: facility_code/year/month/data_{ymdhms}.parquet
            # We filter Inspection by the specific date (T-14).

            # Note: "hourly" implies we might want to be specific about time range,
            # but strftime('%Y-%m-%d') on timestamp column covers the whole day.

            # Query using default aggregation logic (panel_addr)

            # Query:
            # 1. grouped_defects: Group by product_id AND defect_name.
            # 2. Join History (Left) -> Defects.
            # 3. Handle NULL defect_name (No Defects found) -> 'NO_DEFECT' for PK safety.

            query = f"""
                INSERT INTO glass_stats (
                    product_id, defect_name, model_code, lot_id, work_date, 
                    total_defects, panel_addrs, created_at
                )
                WITH target_inspection AS (
                    SELECT 
                        product_id, 
                        defect_name,
                        panel_addr,
                        inspection_end_ymdhms
                    FROM read_parquet([
                        '{inspection_root}/facility_code={facility}/*/*/inspection_data_*.parquet'
                    ], hive_partitioning=true)
                    WHERE 
                        strftime(inspection_end_ymdhms, '%Y-%m-%d') = '{target_date_str}'
                        AND defect_name IS NOT NULL
                ),
                target_history AS (
                    SELECT 
                        product_id, 
                        model_code,
                        lot_id,
                        move_in_ymdhms
                    FROM read_parquet('{history_root}/**/*.parquet', hive_partitioning=true)
                    WHERE facility_code = '{facility}'
                ),
                grouped_defects AS (
                    SELECT
                        product_id,
                        defect_name,
                        list(panel_addr) as panel_addrs,
                        COUNT(panel_addr) as total_defects
                    FROM target_inspection
                    WHERE panel_addr IS NOT NULL
                    GROUP BY product_id, defect_name
                )
                SELECT 
                    h.product_id,
                    COALESCE(d.defect_name, 'NO_DEFECT') as defect_name,
                    COALESCE(h.model_code, 'UNKNOWN') as model_code,
                    h.lot_id,
                    CAST(h.move_in_ymdhms AS DATE) as work_date,
                    COALESCE(d.total_defects, 0) as total_defects,
                    COALESCE(d.panel_addrs, []) as panel_addrs,
                    now() as created_at
                FROM target_history h
                LEFT JOIN grouped_defects d ON h.product_id = d.product_id
                WHERE strftime(h.move_in_ymdhms, '%Y-%m-%d') = '{target_date_str}'
                ON CONFLICT (product_id, defect_name) DO UPDATE SET 
                    model_code = EXCLUDED.model_code,
                    lot_id = EXCLUDED.lot_id,
                    total_defects = EXCLUDED.total_defects,
                    panel_addrs = EXCLUDED.panel_addrs,
                    created_at = now()
            """

            try:
                con.execute(query)
                logger.info(f"Completed analysis for {target_date_str}")
            except Exception as e:
                logger.error(f"Analysis failed for {target_date_str}: {e}")

            curr += timedelta(days=1)

    except Exception as e:
        logger.error(f"DuckDB Error: {e}")
    finally:
        con.close()


if __name__ == "__main__":
    main()
