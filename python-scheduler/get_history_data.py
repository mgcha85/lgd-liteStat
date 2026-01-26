import argparse
import os
import yaml
import logging
from datetime import datetime, timedelta
import pandas as pd
from dotenv import load_dotenv

from dynamodb import DynamoDB, load_column_info, build_expressions, items2df

# Setup Logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger("get_history_data")

load_dotenv()

# Env
AWS_ACCESS_KEY_ID = os.getenv("AWS_ACCESS_KEY_ID")
AWS_SECRET_ACCESS_KEY = os.getenv("AWS_SECRET_ACCESS_KEY")
AWS_REGION = os.getenv("AWS_REGION", "ap-northeast-2")
DYNAMODB_ENDPOINT = os.getenv(
    "DYNAMODB_ENDPOINT", "http://dynamodb.api.lgddna.kr/dynamodb"
)
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
    parser = argparse.ArgumentParser(description="Download History Data")
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
    if not config:
        return

    ingest_cfg = config.get("ingest", {})
    table_name = ingest_cfg.get("history_table")
    columns = ingest_cfg.get("history_columns", [])

    if not table_name:
        logger.error("No history_table defined in config")
        return

    dynamodb = DynamoDB(
        access_key=AWS_ACCESS_KEY_ID,
        secret_key=AWS_SECRET_ACCESS_KEY,
        endpoint=DYNAMODB_ENDPOINT,
        region=AWS_REGION,
    )

    # from utils import save_to_parquet

    curr = start_date
    while curr <= end_date:
        logger.info(f"Processing {curr.date()} for {facility}...")

        conditions = {
            "facility_code": f"= '{facility}'",
            # Assuming time_ymdhms key for history
            "time_ymdhms": f"begins_with '{curr.strftime('%Y-%m-%d')}'",
        }

        try:
            schema = load_column_info(table_name=table_name)
            exprs = build_expressions(table_name, schema, columns, conditions)

            response = dynamodb.query(**exprs)
            df = items2df(response.get("Items"), schema, columns, conditions)

            # Preprocess
            if "move_in_ymdhms" in df.columns:
                df["move_in_ymdhms"] = pd.to_datetime(
                    df["move_in_ymdhms"], errors="coerce"
                )

            # Sort by product_id for better index performance in DuckDB (Zone Maps)
            if "product_id" in df.columns:
                df = df.sort_values(by="product_id")

            # Direct DuckDB Insertion
            import duckdb

            # Use DB_PATH from env or default to shared volume path
            db_path = os.getenv("DB_PATH", "/app/data/analytics.duckdb")

            con = duckdb.connect(db_path)

            # Create table if not exists (using DataFrame schema inferred by DuckDB)
            # We use 'create or replace' or 'insert into'?
            # Since it's history, we append.
            # But we must ensure table exists.

            try:
                # Create table structure if not exists
                # We can use the DF to create a template if needed, or rely on 'CREATE TABLE IF NOT EXISTS'
                # But best way with DF is:
                con.execute(
                    f"CREATE TABLE IF NOT EXISTS history AS SELECT * FROM df LIMIT 0"
                )

                # Create Index if not exists
                con.execute(
                    "CREATE INDEX IF NOT EXISTS idx_history_product_id ON history(product_id)"
                )

                # Insert data
                con.execute("INSERT INTO history SELECT * FROM df")
                logger.info(f"Inserted {len(df)} rows into DuckDB history table.")

                # ---------------------------------------------------------
                # Daily Batch Analysis: Join History (DuckDB) + Inspection (Parquet)
                # target: glass_stats
                # ---------------------------------------------------------

                # 1. Ensure Analysis Table Exists
                con.execute("""
                    CREATE TABLE IF NOT EXISTS glass_stats (
                        product_id TEXT PRIMARY KEY,
                        lot_id TEXT,
                        product_model_code TEXT,
                        work_date DATE,
                        total_defects INTEGER,
                        created_at TIMESTAMP
                    )
                """)

                # 2. Perform Join & Aggregation
                # Check available columns in history table to avoid errors
                cols = df.columns.tolist()
                p_id = "product_id" if "product_id" in cols else "glass_id"
                l_id = "lot_id" if "lot_id" in cols else "NULL"
                m_code = (
                    "product_model_code" if "product_model_code" in cols else "NULL"
                )
                w_date = "move_in_ymdhms" if "move_in_ymdhms" in cols else "time_ymdhms"

                target_day = curr.strftime("%Y-%m-%d")

                analysis_sql = f"""
                    INSERT INTO glass_stats
                    SELECT 
                        h.{p_id} as product_id,
                        {l_id} as lot_id,
                        {m_code} as product_model_code,
                        CAST(h.{w_date} AS DATE) as work_date,
                        COUNT(i.product_id) as total_defects,
                        CURRENT_TIMESTAMP as created_at
                    FROM history h
                    LEFT JOIN read_parquet('{DATA_DIR}/inspection/**/*.parquet', hive_partitioning=true) i
                    ON h.{p_id} = i.product_id
                    WHERE strftime(CAST(h.{w_date} AS DATE), '%Y-%m-%d') = '{target_day}'
                    GROUP BY 1, 2, 3, 4
                    ON CONFLICT (product_id) DO UPDATE SET 
                        total_defects = EXCLUDED.total_defects,
                        created_at = CURRENT_TIMESTAMP
                """

                logger.info(f"Running Analysis Batch for {target_day}...")
                con.execute(analysis_sql)
                logger.info("Analysis Batch Completed.")

            except Exception as e:
                logger.error(f"DuckDB Operation Error: {e}")
            finally:
                con.close()

        except Exception as e:
            logger.error(f"Failed to process {curr.date()}: {e}")

        curr += timedelta(days=1)


if __name__ == "__main__":
    main()
