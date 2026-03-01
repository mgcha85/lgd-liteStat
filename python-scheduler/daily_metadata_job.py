import argparse
import os
import logging
import requests
from datetime import datetime
from dotenv import load_dotenv

# Setup Logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger("daily_metadata_job")

load_dotenv()

# Go backend URL - from env or default to Docker service hostname
BACKEND_URL = os.getenv("BACKEND_URL", "http://litestat-backend:8080")


def call_metadata_update(facility: str, target_date: str, timeout: int = 120) -> bool:
    """
    Calls Go backend POST /api/jobs/metadata to perform DuckDB metadata update.
    This replaces the previous direct DuckDB connection approach to avoid
    file lock conflicts (Go backend holds the DuckDB connection exclusively).
    """
    url = f"{BACKEND_URL}/api/jobs/metadata"
    params = {"facility": facility, "target_date": target_date}

    logger.info(f"Calling Go API: POST {url} params={params}")

    try:
        resp = requests.post(url, params=params, timeout=timeout)
        resp.raise_for_status()
        data = resp.json()
        logger.info(
            f"Metadata update OK: facility={data.get('facility')}, "
            f"date={data.get('target_date')}, status={data.get('status')}"
        )
        return True
    except requests.exceptions.ConnectionError:
        logger.error(f"Cannot connect to Go backend at {url}. Is it running?")
    except requests.exceptions.Timeout:
        logger.error(f"Go backend timed out after {timeout}s")
    except requests.exceptions.HTTPError as e:
        logger.error(
            f"Go backend returned error: {e.response.status_code} {e.response.text}"
        )
    except Exception as e:
        logger.error(f"Unexpected error calling metadata API: {e}")

    return False


def main():
    parser = argparse.ArgumentParser(
        description="Run Daily Metadata Update via Go Backend API"
    )
    parser.add_argument("--start", type=str, required=True, help="YYYY-MM-DD")
    parser.add_argument(
        "--end",
        type=str,
        required=True,
        help="YYYY-MM-DD (currently unused, same as start)",
    )
    parser.add_argument("--fac", type=str, required=True, help="Facility Code")
    args = parser.parse_args()

    start_date = datetime.strptime(args.start, "%Y-%m-%d")
    facility = args.fac
    target_date_str = start_date.strftime("%Y-%m-%d")

    logger.info(
        f"Starting metadata update for facility={facility}, date={target_date_str}"
    )

    success = call_metadata_update(facility, target_date_str)

    if success:
        logger.info(f"Metadata update completed for {target_date_str}")
    else:
        logger.error(f"Metadata update FAILED for {target_date_str}")
        raise SystemExit(1)


if __name__ == "__main__":
    main()
