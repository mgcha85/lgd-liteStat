import os
import time
import schedule
import logging
import subprocess
import yaml
from datetime import datetime, timedelta
from dotenv import load_dotenv

# Setup Logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger("scheduler")

load_dotenv()
SCHEDULE_TIME = os.getenv("SCHEDULE_TIME", "02:00")
CONFIG_PATH = os.getenv("CONFIG_PATH", "config.yaml")


def load_app_config():
    try:
        with open(CONFIG_PATH, "r") as f:
            return yaml.safe_load(f)
    except Exception as e:
        logger.error(f"Failed to load config.yaml: {e}")
        return None


def run_daily_tasks():
    logger.info("Starting Daily Tasks...")

    config = load_app_config()
    if not config:
        return

    facilities = config.get("settings", {}).get("facilities", [])

    # Target: Yesterday
    yesterday = datetime.now() - timedelta(days=1)
    date_str = yesterday.strftime("%Y-%m-%d")

    for fac in facilities:
        logger.info(f"Running tasks for {fac} date {date_str}")

        # 1. Inspection Data
        cmd_insp = [
            "python",
            "get_insp_data.py",
            "--start",
            date_str,
            "--end",
            date_str,
            "--fac",
            fac,
        ]
        try:
            subprocess.run(cmd_insp, check=True)
            logger.info("Inspection download success")
        except subprocess.CalledProcessError as e:
            logger.error(f"Inspection download failed: {e}")

        # 2. History Data
        cmd_hist = [
            "python",
            "get_history_data.py",
            "--start",
            date_str,
            "--end",
            date_str,
            "--fac",
            fac,
        ]
        try:
            subprocess.run(cmd_hist, check=True)
            logger.info("History download success")
        except subprocess.CalledProcessError as e:
            logger.error(f"History download failed: {e}")

    logger.info("Daily Tasks Completed.")


def run_scheduler():
    logger.info(f"Scheduler started. Scheduled at {SCHEDULE_TIME}")
    schedule.every().day.at(SCHEDULE_TIME).do(run_daily_tasks)

    while True:
        schedule.run_pending()
        time.sleep(60)


if __name__ == "__main__":
    # Test run on startup (Optional)
    # run_daily_tasks()
    run_scheduler()
