from fastapi import FastAPI, UploadFile, File, Form, HTTPException
from fastapi.responses import FileResponse, JSONResponse
import polars as pl
import yaml
import shutil
import os
import tempfile
import duckdb

from logic import (
    preprocess_defects,
    load_and_filter_pnl_map,
    calculate_gap_shifts,
    apply_gap_removal_and_grid,
    rename_panels_no_grid,
)

app = FastAPI(title="LGD Canvas Analysis")


def load_config():
    with open("config.yaml", "r") as f:
        return yaml.safe_load(f)


config = load_config()


@app.post("/analyze")
async def analyze_defects(
    defect_file: UploadFile = File(...),
    pnl_map_file: UploadFile = File(...),
    facility_code: str = Form(...),
    part_no_name: str = Form(...),
    N: int = Form(None),
    M: int = Form(None),
):
    if N is None:
        N = config["grid"]["N"]
    if M is None:
        M = config["grid"]["M"]

    with tempfile.TemporaryDirectory() as temp_dir:
        defect_path = os.path.join(temp_dir, "defect.parquet")
        pnl_map_path = os.path.join(temp_dir, "pnl_map.parquet")

        with open(defect_path, "wb") as f:
            shutil.copyfileobj(defect_file.file, f)

        with open(pnl_map_path, "wb") as f:
            shutil.copyfileobj(pnl_map_file.file, f)

        try:
            defect_df = pl.scan_parquet(defect_path)
            defect_df = preprocess_defects(defect_df)

            pnl_map_df = load_and_filter_pnl_map(
                pnl_map_path, facility_code, part_no_name
            )

            if pnl_map_df.height == 0:
                raise HTTPException(
                    status_code=400, detail="No matching panel map data found."
                )

            panel_shifts, x_shifts, y_shifts = calculate_gap_shifts(pnl_map_df)

            defect_df_collected = defect_df.collect()

            result_df = apply_gap_removal_and_grid(
                defect_df_collected, panel_shifts, N, M
            )

            os.makedirs("output", exist_ok=True)
            final_path = f"output/processed_{facility_code}_{part_no_name}.parquet"
            result_df.write_parquet(final_path)

            return FileResponse(
                final_path,
                filename="processed_defect.parquet",
                media_type="application/octet-stream",
            )

        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))


@app.post("/analyze/grid")
async def analyze_defects_grid(
    defect_file: UploadFile = File(...),
    facility_code: str = Form(...),
    part_no_name: str = Form(...),
    N: int = Form(0),
    M: int = Form(0),
):
    # N=0 or M=0 means: keep original panel structure, just rename with new addr labels
    use_grid = N > 0 and M > 0

    db_path = os.path.abspath(
        os.path.join(
            os.environ.get(
                "LAKE_DIR",
                os.path.join(os.path.dirname(__file__), "..", "data", "lake"),
            ),
            f"{facility_code}.duckdb",
        )
    )

    if not os.path.exists(db_path):
        raise HTTPException(
            status_code=400,
            detail=f"Database not found for facility: {facility_code} at {db_path}",
        )

    with tempfile.TemporaryDirectory() as temp_dir:
        defect_path = os.path.join(temp_dir, "defect.parquet")

        with open(defect_path, "wb") as f:
            shutil.copyfileobj(defect_file.file, f)

        try:
            defect_df = pl.scan_parquet(defect_path)
            defect_df = preprocess_defects(defect_df)

            # Query DuckDB
            con = duckdb.connect(db_path, read_only=True)
            try:
                query = f"SELECT * FROM pnl_map WHERE facility_code = '{facility_code}' AND use_flag = 'Y' AND part_no_name = '{part_no_name}'"
                pnl_map_df = con.execute(query).pl()
            except Exception as e:
                con.close()
                raise HTTPException(
                    status_code=500, detail=f"DuckDB Query Error: {str(e)}"
                )

            con.close()

            if pnl_map_df.height == 0:
                raise HTTPException(
                    status_code=400,
                    detail="No matching panel map data found in DuckDB.",
                )

            # Process same as before
            pnl_map_df = pnl_map_df.with_columns(
                [
                    pl.col("x_coordinate_value")
                    .cast(pl.Float64, strict=False)
                    .round(3)
                    .fill_null(0.0),
                    pl.col("y_coordinate_value")
                    .cast(pl.Float64, strict=False)
                    .round(3)
                    .fill_null(0.0),
                ]
            )

            panel_shifts, x_shifts, y_shifts, gapless_bounds = calculate_gap_shifts(
                pnl_map_df
            )

            defect_df_collected = defect_df.collect()

            if use_grid:
                result_df = apply_gap_removal_and_grid(
                    defect_df_collected, panel_shifts, N, M, gapless_bounds
                )
            else:
                # N=0 or M=0: keep original panels, just rename addresses
                result_df = rename_panels_no_grid(defect_df_collected, pnl_map_df)

            os.makedirs("output", exist_ok=True)
            final_path = f"output/processed_{facility_code}_{part_no_name}.parquet"
            result_df.write_parquet(final_path)

            return FileResponse(
                final_path,
                filename="processed_defect.parquet",
                media_type="application/octet-stream",
            )

        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))


@app.post("/analyze/grid/products")
async def extract_products_by_grid(
    facility_code: str = Form(...),
    part_no_name: str = Form(...),
    defect_file: UploadFile = File(...),
    use_grid: bool = Form(True),
    N: int = Form(10),
    M: int = Form(20),
    target_panels: str = Form(...),  # JSON string array of panels e.g. '["1a", "5c"]'
):
    import json

    try:
        panels_list = json.loads(target_panels)
    except json.JSONDecodeError:
        raise HTTPException(
            status_code=400, detail="target_panels must be a valid JSON string array"
        )

    db_path = os.path.abspath(
        os.path.join(
            os.environ.get(
                "LAKE_DIR",
                os.path.join(os.path.dirname(__file__), "..", "data", "lake"),
            ),
            f"{facility_code}.duckdb",
        )
    )

    if not os.path.exists(db_path):
        raise HTTPException(
            status_code=400,
            detail=f"Database not found for facility: {facility_code} at {db_path}",
        )

    with tempfile.TemporaryDirectory() as temp_dir:
        defect_path = os.path.join(temp_dir, "defect.parquet")

        with open(defect_path, "wb") as f:
            shutil.copyfileobj(defect_file.file, f)

        try:
            defect_df = pl.scan_parquet(defect_path)
            defect_df = preprocess_defects(defect_df)

            # Query DuckDB
            con = duckdb.connect(db_path, read_only=True)
            try:
                query = f"SELECT * FROM pnl_map WHERE facility_code = '{facility_code}' AND use_flag = 'Y' AND part_no_name = '{part_no_name}'"
                pnl_map_df = con.execute(query).pl()
            except Exception as e:
                con.close()
                raise HTTPException(
                    status_code=500, detail=f"DuckDB Query Error: {str(e)}"
                )

            con.close()

            if pnl_map_df.height == 0:
                raise HTTPException(
                    status_code=400,
                    detail="No matching panel map data found in DuckDB.",
                )

            pnl_map_df = pnl_map_df.with_columns(
                [
                    pl.col("x_coordinate_value")
                    .cast(pl.Float64, strict=False)
                    .round(3)
                    .fill_null(0.0),
                    pl.col("y_coordinate_value")
                    .cast(pl.Float64, strict=False)
                    .round(3)
                    .fill_null(0.0),
                ]
            )

            panel_shifts, x_shifts, y_shifts, gapless_bounds = calculate_gap_shifts(
                pnl_map_df
            )

            defect_df_collected = defect_df.collect()

            if use_grid:
                result_df = apply_gap_removal_and_grid(
                    defect_df_collected, panel_shifts, N, M, gapless_bounds
                )
            else:
                result_df = rename_panels_no_grid(defect_df_collected, pnl_map_df)

            # Filter for requested panels and ensure defects exist
            # Note: dense backfill causes 0-defect panels to have null n_defect
            filtered_df = result_df.filter(
                pl.col("sub_panel_addr").is_in(panels_list)
                & pl.col("n_defect").is_not_null()
                & (pl.col("n_defect") > 0)
            )

            # Some files might not have product_id if they are heavily truncated,
            # but standard litestat flow guarantees product_id.
            if "product_id" not in filtered_df.columns:
                raise HTTPException(
                    status_code=400, detail="Missing product_id column in defect data"
                )

            product_ids = (
                filtered_df.select("product_id").unique().to_series().to_list()
            )

            return JSONResponse({"product_ids": product_ids})

        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))
