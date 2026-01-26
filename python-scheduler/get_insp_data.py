import argparse
import os
import yaml
import logging
from datetime import datetime, timedelta
from dotenv import load_dotenv

from dynamodb import DynamoDB, load_column_info, build_expressions, items2df
# Reuse save_to_parquet from snippet or creating utils?
# Since user asked for standalone script, I will duplicate or put utils in common file.
# I will make `utils.py` for common logic.

# Setup Logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger("get_insp_data")

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


import pandas as pd


def preprocess_df(df):
    if df.empty:
        return df

    # 1. inspection_end_ymdhms to datetime
    if "inspection_end_ymdhms" in df.columns:
        df["inspection_end_ymdhms"] = pd.to_datetime(
            df["inspection_end_ymdhms"], errors="coerce"
        )

    # 2. Numeric conversion for def_pnt_*, def_size (strict=False -> errors='coerce')
    numeric_cols = ["def_pnt_x", "def_pnt_y", "def_pnt_g", "def_pnt_d", "def_size"]
    for col in numeric_cols:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")

    # 3. def_latest_summary_defect_term_name_s parsing
    # Split by '-', take 2nd (idx 1) and 4th (idx 3), join by '-'
    target_col = "def_latest_summary_defect_term_name_s"
    if target_col in df.columns:

        def parse_defect_name(val):
            if not isinstance(val, str):
                return None
            parts = val.split("-")
            if len(parts) >= 4:
                # Assuming 1-based index from user description: "2nd, 4th" -> index 1, 3
                # User said joined by "- ", so split("-") gives " term".
                # We will strip whitespace to be clean, or should we keep it raw?
                # Usually preprocessing implies cleaning. I'll strip.
                p1 = parts[1].strip()
                p2 = parts[3].strip()
                return f"{p1}-{p2}"
            return None

        df["defect_name"] = df[target_col].apply(parse_defect_name)

    return df


def main():
    parser = argparse.ArgumentParser(description="Download Inspection Data")
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
    table_name = ingest_cfg.get("inspection_table")
    columns = ingest_cfg.get("inspection_columns", [])

    if not table_name:
        logger.error("No inspection_table defined in config")
        return

    dynamodb = DynamoDB(
        access_key=AWS_ACCESS_KEY_ID,
        secret_key=AWS_SECRET_ACCESS_KEY,
        endpoint=DYNAMODB_ENDPOINT,
        region=AWS_REGION,
    )

    # Import save util
    from utils import save_to_parquet

    curr = start_date
    while curr <= end_date:
        logger.info(f"Processing {curr.date()} for {facility}...")

        # Condition
        # Assuming table has facility_code and time-based field
        conditions = {
            "facility_code": f"= '{facility}'",
            # We assume inspection_end_ymdhms is the key as per config
            "inspection_end_ymdhms": f"begins_with '{curr.strftime('%Y-%m-%d')}'",
        }

        try:
            schema = load_column_info(table_name=table_name)
            exprs = build_expressions(table_name, schema, columns, conditions)

            response = dynamodb.query(**exprs)
            df = items2df(response.get("Items"), schema, columns, conditions)

            # Preprocess
            df = preprocess_df(df)

            save_to_parquet(df, "inspection", facility, curr)

        except Exception as e:
            logger.error(f"Failed to process {curr.date()}: {e}")

        curr += timedelta(days=1)


if __name__ == "__main__":
    main()
