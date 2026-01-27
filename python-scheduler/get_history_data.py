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
            if "model_code" not in df.columns:
                df["model_code"] = df["product_id"].str[2:4]
            else:
                df["model_code"] = df["model_code"].fillna("UNKNOWN")

            if "lot_id" not in df.columns:
                df["lot_id"] = df["product_id"].str[:-2]

            # 3. Write to Parquet with Bloom Filter
            # Partitioning: facility_code -> model_code -> bucket_id
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

            # Prepare arguments for write_to_dataset
            write_kwargs = {
                "root_path": history_root,
                "partition_cols": ["facility_code", "model_code", "bucket_id"],
                "basename_template": f"{date_str}_{{i}}.parquet",
                "existing_data_behavior": "overwrite_or_ignore",
            }

            # Check if ParquetFileOptions is available (PyArrow >= 13.0.0)
            if hasattr(pq, "ParquetFileOptions"):
                try:
                    write_kwargs["file_options"] = pq.ParquetFileOptions(
                        bloom_filter_level="default",
                        bloom_filter_columns={"product_id"},
                    )
                except Exception as e:
                    logger.warning(f"Failed to configure Bloom Filter: {e}")

            pq.write_to_dataset(table, **write_kwargs)

            logger.info(f"Saved Hash-Bucketed Parquet for {date_str}")

            # ---------------------------------------------------------
            # Daily Batch Analysis: Join History (Parquet) + Inspection (Parquet)
            # target: glass_stats
            # ---------------------------------------------------------

            # Direct DuckDB Insertion Logic Removed (Replaced by Parquet)
            # Now we use DuckDB to Query the PARQUET files we just wrote.

        except Exception as e:
            logger.error(f"Failed to process {curr.date()}: {e}")

        curr += timedelta(days=1)


if __name__ == "__main__":
    main()
