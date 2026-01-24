import os
import logging
from pathlib import Path
import pandas as pd

logger = logging.getLogger("scheduler.utils")
DATA_DIR = os.getenv("DATA_DIR", "/app/data/lake")


def save_to_parquet(df, table_type, facility, target_date):
    if df.empty:
        logger.warning(f"No data to save for {target_date.date()}")
        return

    year = target_date.year
    month = target_date.month
    day_str = target_date.strftime("%Y%m%d")

    # Hive Partition: table/facility_code=.../year=.../month=...
    partition_path = (
        Path(DATA_DIR)
        / table_type
        / f"facility_code={facility}"
        / f"year={year}"
        / f"month={month}"
    )
    partition_path.mkdir(parents=True, exist_ok=True)

    file_name = f"{table_type}_data_{day_str}.parquet"
    full_path = partition_path / file_name

    try:
        # Use pyarrow engine
        df.to_parquet(full_path, index=False, engine="pyarrow")
        logger.info(f"Saved {len(df)} rows to {full_path}")
    except Exception as e:
        logger.error(f"Failed to save parquet: {e}")
