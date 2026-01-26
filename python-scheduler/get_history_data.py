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

            # ---------------------------------------------------------
            # Hash Bucketing + Bloom Filter Storage Strategy
            # Structure: facility_code / model / bucket / date.parquet
            # ---------------------------------------------------------
            import hashlib
            import pyarrow as pa
            import pyarrow.parquet as pq

            # 1. Add Bucket ID
            def get_bucket(val):
                if not isinstance(val, str):
                    return "00"
                # Simple consistent hash: MD5 mod 100
                h = int(hashlib.md5(val.encode("utf-8")).hexdigest(), 16)
                return f"{h % 100:02d}"

            if "product_id" in df.columns:
                df["bucket_id"] = df["product_id"].apply(get_bucket)
            else:
                df["bucket_id"] = "00"

            # 2. Add Partition Columns if missing
            if "facility_code" not in df.columns:
                df["facility_code"] = facility  # Should be 'P8' etc.

            # Ensure model code exists (fill 'UNKNOWN' if missing)
            if "product_model_code" not in df.columns:
                df["product_model_code"] = "UNKNOWN"
            else:
                df["product_model_code"] = df["product_model_code"].fillna("UNKNOWN")

            # 3. Write to Parquet with Bloom Filter
            # Partitioning: facility_code -> product_model_code -> bucket_id
            # Filename: YYYY-MM-DD.parquet inside the bucket folder

            table = pa.Table.from_pandas(df)

            # Define File Options with Bloom Filter
            # Enable BF for product_id
            # Note: pyarrow.parquet.write_to_dataset uses 'file_options' in recent versions,
            # or we can write manually. Given the specific structure requested:
            # facility_code=.../model=.../bucket=.../YYYY-MM-DD.parquet
            # We can use write_to_dataset with partition_cols.

            # But the user requested "date.parquet" filename specifically.
            # write_to_dataset usually generates "part-{i}.parquet" or similar.
            # We can use basename_template to force a name prefix, but getting exactly "2023-10-27.parquet"
            # might require manual partition iteration if we want simple filenames.
            # However, standard Hive partitioning is usually directory-based.
            # User example: /data/.../bucket=01/2023-10-27.parquet
            # We will use write_to_dataset with basename_template setting.

            date_str = curr.strftime("%Y-%m-%d")

            # Use 'history' subdir in DATA_DIR
            history_root = os.path.join(DATA_DIR, "history")

            pq.write_to_dataset(
                table,
                root_path=history_root,
                partition_cols=["facility_code", "product_model_code", "bucket_id"],
                basename_template=f"{date_str}_{{i}}.parquet",
                existing_data_behavior="overwrite_or_ignore",
                file_options=pq.ParquetFileOptions(
                    bloom_filter_level="default", bloom_filter_columns={"product_id"}
                ),
            )

            logger.info(f"Saved Hash-Bucketed Parquet for {date_str}")

            # ---------------------------------------------------------
            # Daily Batch Analysis: Join History (Parquet) + Inspection (Parquet)
            # target: glass_stats
            # ---------------------------------------------------------

            # Direct DuckDB Insertion Logic Removed (Replaced by Parquet)
            # Now we use DuckDB to Query the PARQUET files we just wrote.

            import duckdb

            # Use DB_PATH from env or default to shared volume path
            db_path = os.getenv("DB_PATH", "/app/data/analytics.duckdb")
            con = duckdb.connect(db_path)

            try:
                # 1. Ensure Analysis Table Exists & Has Correct Schema
                con.execute("""
                    CREATE TABLE IF NOT EXISTS glass_stats (
                        product_id TEXT PRIMARY KEY,
                        lot_id TEXT,
                        product_model_code TEXT,
                        work_date DATE,
                        total_defects INTEGER,
                        panel_map INTEGER[], -- Nested structure for panel defect map
                        panel_addrs TEXT[],  -- Nested structure for raw panel addresses
                        created_at TIMESTAMP
                    )
                """)

                # Migration check for panel_addrs
                try:
                    con.execute("ALTER TABLE glass_stats ADD COLUMN panel_addrs TEXT[]")
                except Exception:
                    pass

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

                # Logic refers to backend/mart/mart.go
                # We calculate defect_indices from panel_addr and map to 260 array
                # Added panel_addrs list aggregation
                # MODIFIED: Read from History Parquet instead of Table
                analysis_sql = f"""
                    INSERT INTO glass_stats (
                        product_id, lot_id, product_model_code, work_date, 
                        total_defects, panel_map, panel_addrs, created_at
                    )
                    WITH glass_defects AS (
                        SELECT 
                            product_id,
                            list_distinct(list(
                                CASE WHEN panel_addr IS NOT NULL AND LENGTH(panel_addr) >= 2 THEN
                                    (ascii(SUBSTR(panel_addr, 1, 1)) - 65) * 10 + 
                                    CAST(SUBSTR(panel_addr, 2, 1) AS INTEGER) + 1
                                ELSE NULL END
                            )) as defect_indices,
                            list(panel_addr) as panel_addrs
                        FROM read_parquet('{DATA_DIR}/inspection/**/*.parquet', hive_partitioning=true)
                        WHERE panel_addr IS NOT NULL
                        GROUP BY product_id
                    ),
                    history_source AS (
                        SELECT * 
                        FROM read_parquet('{history_root}/**/*.parquet', hive_partitioning=true)
                        WHERE facility_code = '{facility}'
                    )
                    SELECT 
                        h.{p_id} as product_id,
                        {l_id} as lot_id,
                        {m_code} as product_model_code,
                        CAST(h.{w_date} AS DATE) as work_date,
                        COALESCE(len(d.defect_indices), 0) as total_defects,
                        list_transform(range(1, 261), idx -> 
                            CASE WHEN list_contains(COALESCE(d.defect_indices, []), idx) THEN 1 
                            ELSE 0 END
                        ) as panel_map,
                        COALESCE(d.panel_addrs, []) as panel_addrs,
                        CURRENT_TIMESTAMP as created_at
                    FROM history_source h
                    LEFT JOIN glass_defects d ON h.{p_id} = d.product_id
                    WHERE strftime(CAST(h.{w_date} AS DATE), '%Y-%m-%d') = '{target_day}'
                    ON CONFLICT (product_id) DO UPDATE SET 
                        total_defects = EXCLUDED.total_defects,
                        panel_map = EXCLUDED.panel_map,
                        panel_addrs = EXCLUDED.panel_addrs,
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
