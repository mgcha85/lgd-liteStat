import argparse
import os
import yaml
import logging
from datetime import datetime
from dotenv import load_dotenv
import duckdb

# Setup Logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger("daily_metadata_job")

load_dotenv()

DATA_DIR = os.getenv("DATA_DIR", "/app/data/lake")
CONFIG_PATH = os.getenv("CONFIG_PATH", "config.yaml")


def load_app_config():
    try:
        with open(CONFIG_PATH, "r") as f:
            return yaml.safe_load(f)
    except Exception as e:
        logger.error(f"Failed to load config.yaml: {e}")
        return None


def update_masters(con, history_root, inspection_root, facility, target_date_str):
    """
    1. Update Model Master (Unique models from History)
    2. Update Defect Master (Unique defects from Inspection)
    """
    logger.info("Updating Model and Defect Masters...")

    con.execute("""
        CREATE TABLE IF NOT EXISTS model_master (
            model_code TEXT PRIMARY KEY,
            updated_at TIMESTAMP
        );
        CREATE TABLE IF NOT EXISTS defect_master (
            defect_name TEXT PRIMARY KEY,
            updated_at TIMESTAMP
        );
    """)

    try:
        # 1. Model Master
        con.execute(f"""
            INSERT INTO model_master (model_code, updated_at)
            SELECT DISTINCT model_code, now()
            FROM read_parquet('{history_root}/**/*.parquet', hive_partitioning=true)
            WHERE facility_code = '{facility}' 
              AND strftime(move_in_ymdhms, '%Y-%m-%d') = '{target_date_str}'
            ON CONFLICT (model_code) DO UPDATE SET updated_at = now()
        """)

        # 2. Defect Master
        con.execute(f"""
            INSERT INTO defect_master (defect_name, updated_at)
            SELECT DISTINCT defect_name, now()
            FROM read_parquet([
                '{inspection_root}/facility_code={facility}/*/*/inspection_data_*.parquet'
            ], hive_partitioning=true)
            WHERE strftime(inspection_end_ymdhms, '%Y-%m-%d') = '{target_date_str}'
              AND defect_name IS NOT NULL
            ON CONFLICT (defect_name) DO UPDATE SET updated_at = now()
        """)
        logger.info("Master Tables Initialized/Updated.")
    except Exception as e:
        logger.error(f"Failed to update Masters: {e}")


def update_model_layout_link(con):
    """
    3. Link Model -> Layout Info (Daily Ref)
    Simplified Logic: Join 'part_no' and 'pnl_map' to cache Model -> Ref Panels.
    Assumes:
      - 'part_no' table has columns: model_code, part_no_name
      - 'pnl_map' table has columns: part_no_name, ref_panels (List[str])
    """
    logger.info("Updating Model Layout Master...")

    # We create a specific table for Model -> Layout Mapping
    con.execute("""
        CREATE TABLE IF NOT EXISTS model_layout_master (
            model_code TEXT PRIMARY KEY,
            ref_panels TEXT[],
            updated_at TIMESTAMP
        );
    """)

    # Logic: Join part_no & pnl_map to get Model's Layout.
    # If a model has multiple part_nos with layouts, we assume they are identical
    # (as per user: "same within model"), so we pick ANY_VALUE or MAX.
    try:
        con.execute("""
            INSERT INTO model_layout_master (model_code, ref_panels, updated_at)
            SELECT 
                pn.model_code,
                ANY_VALUE(pm.ref_panels) as ref_panels,
                now()
            FROM part_no pn
            JOIN pnl_map pm ON pn.part_no_name = pm.part_no_name
            WHERE pm.ref_panels IS NOT NULL
            GROUP BY pn.model_code
            ON CONFLICT (model_code) DO UPDATE SET 
                ref_panels = EXCLUDED.ref_panels,
                updated_at = now()
        """)
        logger.info("Model Layout Master Updated.")
    except Exception as e:
        logger.warning(f"Failed to update Layout Master: {e}")
        logger.warning(
            "Ensure 'part_no' (cols: model_code, part_no_name) and 'pnl_map' (cols: part_no_name, ref_panels) tables exist."
        )


def get_full_grid_analysis_query(
    facility, target_date_str, history_root, inspection_root
):
    """
    4. Generates the SQL Query for Full-Grid Analysis.
    Uses 'model_layout_master' to fill 0s for non-defect panels.
    """
    return f"""
        INSERT INTO glass_stats (
            product_id, defect_name, model_code, lot_id, work_date, 
            total_defects, panel_map, panel_addrs, created_at
        )
        WITH target_inspection AS (
            SELECT 
                product_id, defect_name, panel_addr, inspection_end_ymdhms
            FROM read_parquet([
                '{inspection_root}/facility_code={facility}/*/*/inspection_data_*.parquet'
            ], hive_partitioning=true)
            WHERE strftime(inspection_end_ymdhms, '%Y-%m-%d') = '{target_date_str}'
              AND defect_name IS NOT NULL
        ),
        target_history AS (
            SELECT product_id, model_code, lot_id, move_in_ymdhms
            FROM read_parquet('{history_root}/**/*.parquet', hive_partitioning=true)
            WHERE facility_code = '{facility}'
        ),
        inner_stats AS (
            SELECT product_id, defect_name, panel_addr, COUNT(*) as addr_count
            FROM target_inspection
            WHERE panel_addr IS NOT NULL
            GROUP BY product_id, defect_name, panel_addr
        ),
        grouped_defects AS (
            SELECT
                product_id, defect_name, SUM(addr_count) as total_defects,
                list(panel_addr) as obs_addrs,
                list(addr_count) as obs_counts,
                MAP(list(panel_addr), list(addr_count)) as defect_map_obj
            FROM inner_stats
            GROUP BY product_id, defect_name
        ),
        layout_info AS (
            SELECT h.product_id, COALESCE(m.ref_panels, []) as ref_panels
            FROM target_history h
            LEFT JOIN model_layout_master m ON h.model_code = m.model_code
        )
        SELECT 
            d.product_id, d.defect_name,
            COALESCE(h.model_code, 'UNKNOWN') as model_code,
            h.lot_id, CAST(h.move_in_ymdhms AS DATE) as work_date,
            d.total_defects,
            CASE WHEN len(l.ref_panels) > 0 THEN
                list_transform(l.ref_panels, x -> 
                    COALESCE(list_extract(element_at(d.defect_map_obj, x), 1), 0)
                )
            ELSE d.obs_counts END as panel_map,
            CASE WHEN len(l.ref_panels) > 0 THEN l.ref_panels ELSE d.obs_addrs END as panel_addrs,
            now() as created_at
        FROM grouped_defects d
        LEFT JOIN target_history h ON d.product_id = h.product_id
        LEFT JOIN layout_info l ON d.product_id = l.product_id
        ORDER BY d.product_id, d.defect_name
        ON CONFLICT (product_id, defect_name) DO UPDATE SET 
            model_code = EXCLUDED.model_code,
            lot_id = EXCLUDED.lot_id,
            total_defects = glass_stats.total_defects + EXCLUDED.total_defects,
            panel_map = list_concat(glass_stats.panel_map, EXCLUDED.panel_map),
            panel_addrs = list_concat(glass_stats.panel_addrs, EXCLUDED.panel_addrs),
            created_at = now()
    """


def main():
    parser = argparse.ArgumentParser(description="Run Daily Metadata Update Batch")
    parser.add_argument("--start", type=str, required=True, help="YYYY-MM-DD")
    parser.add_argument("--end", type=str, required=True, help="YYYY-MM-DD")
    parser.add_argument("--fac", type=str, required=True, help="Facility Code")
    args = parser.parse_args()

    start_date = datetime.strptime(args.start, "%Y-%m-%d")
    facility = args.fac
    db_file = os.path.join(DATA_DIR, f"{facility}.duckdb")

    con = duckdb.connect(db_file)
    logger.info(f"Connected to {db_file}")

    history_root = os.path.join(DATA_DIR, "history")
    inspection_root = os.path.join(DATA_DIR, "inspection")

    try:
        # Run updates for the start date (or loop range if needed)
        target_date_str = start_date.strftime("%Y-%m-%d")

        # 1 & 2
        update_masters(con, history_root, inspection_root, facility, target_date_str)

        # 3
        update_model_layout_link(con)

        logger.info(f"Metadata update completed for {target_date_str}")

        # 4. Generate Query Example
        q = get_full_grid_analysis_query(
            facility, target_date_str, history_root, inspection_root
        )
        logger.debug("Generated Main Analysis Query:\n" + q)

    except Exception as e:
        logger.error(f"Error: {e}")
    finally:
        con.close()


if __name__ == "__main__":
    main()
