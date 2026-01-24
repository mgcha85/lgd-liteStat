import argparse
import os
import yaml
import logging
from datetime import datetime, timedelta
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

    from utils import save_to_parquet

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

            # Sort by product_id for better index performance in DuckDB (Zone Maps)
            if "product_id" in df.columns:
                df = df.sort_values(by="product_id")

            save_to_parquet(df, "history", facility, curr)

        except Exception as e:
            logger.error(f"Failed to process {curr.date()}: {e}")

        curr += timedelta(days=1)


if __name__ == "__main__":
    main()
