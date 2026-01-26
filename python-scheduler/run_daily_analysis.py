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
DB_PATH = os.getenv("DB_PATH", "/app/data/analytics.duckdb")


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
    if not config:
        return

    # Ingest config might be needed if we want to know column names dynamically,
    # but for now we basically used fixed logic or assumed columns in the previous code.
    # We'll replicate the previous logic.

    # We need to know which columns to use?
    # In get_history_data.py we inferred p_id, l_id etc from the DataFrame 'df'.
    # Here we don't have 'df'. We must rely on the Schema of the Parquet files.
    # We can infer it by reading one file or just using standard names if we trust them.
    # Or, effectively, we validly assume standard schema:
    # product_id, lot_id, product_model_code, time_ymdhms (or move_in_ymdhms)

    # Defaults/Placeholders removed as they are calculated dynamically inside loop

    con = duckdb.connect(DB_PATH)
    history_root = os.path.join(DATA_DIR, "history")

    try:
        # 1. Ensure Analysis Table Exists
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

        # Migration check
        try:
            con.execute(
                "ALTER TABLE glass_stats ADD COLUMN IF NOT EXISTS panel_addrs TEXT[]"
            )
        except Exception:
            try:
                con.execute("ALTER TABLE glass_stats ADD COLUMN panel_addrs TEXT[]")
            except Exception:
                pass

        try:
            con.execute(
                "ALTER TABLE glass_stats ADD COLUMN IF NOT EXISTS panel_map INTEGER[]"
            )
        except Exception:
            try:
                con.execute("ALTER TABLE glass_stats ADD COLUMN panel_map INTEGER[]")
            except Exception:
                pass

        curr = start_date
        while curr <= end_date:
            target_day = curr.strftime("%Y-%m-%d")
            logger.info(
                f"Running Analysis Batch for {target_day} (Facility: {facility})..."
            )

            # We need to handle column names dynamically?
            # In the previous code:
            # col = df.columns.tolist() ...
            # existing parquet schema might be needed.
            # We can peek at schema using LIMIT 1.

            try:
                # Dynamic Column Check using DuckDB distinct from the 'history' parquet
                # We limit to facility and target day to be efficient?
                # Or just read schema from any file in that partition.
                # history_root/facility_code=FAC/...

                # Check if data exists for this facility
                check_sql = f"SELECT * FROM read_parquet('{history_root}/**/*.parquet', hive_partitioning=true) WHERE facility_code='{facility}' LIMIT 1"
                # This might fail if no files exist.
                schema_df = con.execute(check_sql).fetchdf()

                if schema_df.empty:
                    logger.warning(
                        f"No history data found for facility {facility}, skipping analysis for {target_day}"
                    )
                    curr += timedelta(days=1)
                    continue

                cols = schema_df.columns.tolist()
                p_id_col = "product_id" if "product_id" in cols else "glass_id"
                l_id_col = "lot_id" if "lot_id" in cols else "NULL"
                m_code_col = (
                    "product_model_code" if "product_model_code" in cols else "NULL"
                )
                w_date_col = (
                    "move_in_ymdhms" if "move_in_ymdhms" in cols else "time_ymdhms"
                )

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
                        h.{p_id_col} as product_id,
                        {l_id_col} as lot_id,
                        {m_code_col} as product_model_code,
                        CAST(h.{w_date_col} AS DATE) as work_date,
                        COALESCE(len(d.defect_indices), 0) as total_defects,
                        list_transform(range(1, 261), idx -> 
                            CASE WHEN list_contains(COALESCE(d.defect_indices, []), idx) THEN 1 
                            ELSE 0 END
                        ) as panel_map,
                        COALESCE(d.panel_addrs, []) as panel_addrs,
                        CURRENT_TIMESTAMP as created_at
                    FROM history_source h
                    LEFT JOIN glass_defects d ON h.{p_id_col} = d.product_id
                    WHERE strftime(CAST(h.{w_date_col} AS DATE), '%Y-%m-%d') = '{target_day}'
                    ON CONFLICT (product_id) DO UPDATE SET 
                        total_defects = EXCLUDED.total_defects,
                        panel_map = EXCLUDED.panel_map,
                        panel_addrs = EXCLUDED.panel_addrs,
                        created_at = CURRENT_TIMESTAMP
                """

                con.execute(analysis_sql)

                # ---------------------------------------------------------
                # 2. Defect Level Aggregation (glass_defect_stats)
                # ---------------------------------------------------------
                # Group by product_id, defect_name to get counts per defect type
                con.execute("""
                    CREATE TABLE IF NOT EXISTS glass_defect_stats (
                        product_id TEXT,
                        defect_name TEXT,
                        defect_count INTEGER,
                        created_at TIMESTAMP,
                        PRIMARY KEY (product_id, defect_name)
                    )
                """)

                defect_stats_sql = f"""
                    INSERT INTO glass_defect_stats (product_id, defect_name, defect_count, created_at)
                    WITH daily_insp AS (
                        SELECT product_id, defect_name
                        FROM read_parquet('{DATA_DIR}/inspection/**/*.parquet', hive_partitioning=true)
                        WHERE facility_code = '{facility}'
                          AND strftime(CAST(inspection_end_ymdhms AS DATE), '%Y-%m-%d') = '{target_day}'
                          AND defect_name IS NOT NULL
                    ),
                    agg_defects AS (
                        SELECT 
                            product_id, 
                            defect_name, 
                            COUNT(*) as defect_count
                        FROM daily_insp
                        GROUP BY product_id, defect_name
                    )
                    SELECT 
                        product_id,
                        defect_name,
                        defect_count,
                        CURRENT_TIMESTAMP
                    FROM agg_defects
                    ON CONFLICT (product_id, defect_name) DO UPDATE SET
                        defect_count = EXCLUDED.defect_count,
                        created_at = CURRENT_TIMESTAMP
                """
                con.execute(defect_stats_sql)

                logger.info(f"Analysis Batch Completed for {target_day}.")

            except Exception as e:
                logger.error(f"Analysis failed for {target_day}: {e}")

            curr += timedelta(days=1)

    except Exception as e:
        logger.error(f"DuckDB Initialization Error: {e}")
    finally:
        con.close()


if __name__ == "__main__":
    main()
