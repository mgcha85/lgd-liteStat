import polars as pl
import string
from typing import Union


def _label_chars():
    return [str(i) for i in range(1, 10)] + list(string.ascii_lowercase)


def get_label(i: int) -> str:
    chars = _label_chars()
    return chars[i] if 0 <= i < len(chars) else "z"


def rename_panels_no_grid(
    defect_df: pl.DataFrame, pnl_map_df: pl.DataFrame
) -> pl.DataFrame:
    """
    When N=0 or M=0, keep the original panel structure (one cell per panel_location_info)
    but assign new sequential names using the label char sequence.
    Sort panels by their min_x (column), then min_y (row) to determine order.
    """
    panel_bounds = pnl_map_df.group_by("panel_location_info").agg(
        [
            pl.col("x_coordinate_value").min().alias("min_x"),
            pl.col("y_coordinate_value").min().alias("min_y"),
        ]
    )

    # Find unique X columns (sorted)
    x_vals = sorted(panel_bounds["min_x"].unique().to_list())
    y_vals = sorted(panel_bounds["min_y"].unique().to_list())

    x_idx_map = {v: i for i, v in enumerate(x_vals)}
    y_idx_map = {v: i for i, v in enumerate(y_vals)}

    rename_data = []
    for row in panel_bounds.iter_rows(named=True):
        col_idx = x_idx_map.get(row["min_x"], 0)
        row_idx = y_idx_map.get(row["min_y"], 0)
        new_addr = get_label(col_idx) + get_label(row_idx)
        rename_data.append(
            {"panel_addr": row["panel_location_info"], "sub_panel_addr": new_addr}
        )

    rename_df = pl.DataFrame(rename_data)

    # Join new addr to defects using panel_addr
    result = defect_df.join(rename_df, on="panel_addr", how="left")
    result = result.with_columns(
        pl.col("sub_panel_addr").fill_null(pl.col("panel_addr"))
    )
    return result


def preprocess_defects(
    df: Union[pl.DataFrame, pl.LazyFrame],
) -> Union[pl.DataFrame, pl.LazyFrame]:
    numeric_cols = ["def_pnt_x", "def_pnt_y", "def_pnt_g", "def_pnt_d", "def_size"]

    exprs = []
    for col in numeric_cols:
        exprs.append(
            pl.col(col)
            .cast(pl.Float64, strict=False)
            .round(3)
            .fill_null(0.0)
            .alias(col)
        )

    exprs.append(
        pl.col("inspection_end_ymdhms")
        .str.strptime(pl.Datetime, format="%Y%m%d%H%M%S", strict=False)
        .alias("inspection_end_ymdhms")
    )

    # Create panel_addr from panel_id - product_id (A1 from ABCDEFA1 - ABCDEF)
    exprs.append(
        (pl.col("panel_id").str.slice(pl.col("product_id").str.len_bytes())).alias(
            "panel_addr"
        )
    )

    return df.with_columns(exprs)


def load_and_filter_pnl_map(
    pnl_map_path: str, facility_code: str, part_no_name: str
) -> pl.DataFrame:
    df = pl.scan_parquet(pnl_map_path)
    df = df.filter(
        (pl.col("facility_code") == facility_code)
        & (pl.col("use_flag") == "Y")
        & (pl.col("part_no_name") == part_no_name)
    ).collect()

    df = df.with_columns(
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
    return df


def filter_out_of_bound_defects(defect_df: pl.DataFrame, pnl_map_df: pl.DataFrame):
    """
    Remove defects that fall outside the physical boundary of their assigned panel.
    """
    # 1. Get bounds for each panel
    panel_bounds = pnl_map_df.group_by("panel_location_info").agg(
        [
            pl.col("x_coordinate_value").min().alias("pnl_min_x"),
            pl.col("x_coordinate_value").max().alias("pnl_max_x"),
            pl.col("y_coordinate_value").min().alias("pnl_min_y"),
            pl.col("y_coordinate_value").max().alias("pnl_max_y"),
        ]
    )

    # 2. Join defects with panel bounds
    # defect_df has 'panel_addr', pnl_map_df has 'panel_location_info'
    joined = defect_df.join(
        panel_bounds, left_on="panel_addr", right_on="panel_location_info", how="left"
    )

    # 3. Filter
    filtered = joined.filter(
        (pl.col("def_pnt_x") >= pl.col("pnl_min_x"))
        & (pl.col("def_pnt_x") <= pl.col("pnl_max_x"))
        & (pl.col("def_pnt_y") >= pl.col("pnl_min_y"))
        & (pl.col("def_pnt_y") <= pl.col("pnl_max_y"))
    )

    # Clean up boundary columns
    return filtered.drop(["pnl_min_x", "pnl_max_x", "pnl_min_y", "pnl_max_y"])


# Helper to parse 1st and 2nd char
def parse_panel_addr(addr):
    if not addr or len(addr) < 2:
        return None, None
    return addr[0], addr[1]


def calculate_gap_shifts(pnl_map_df: pl.DataFrame):
    # Calculate bounds for each panel_addr
    # pnl_map has 4 rows per panel (corners).
    # We group by panel_location_info (addr)

    panel_bounds = pnl_map_df.group_by("panel_location_info").agg(
        [
            pl.col("x_coordinate_value").min().alias("min_x"),
            pl.col("x_coordinate_value").max().alias("max_x"),
            pl.col("y_coordinate_value").min().alias("min_y"),
            pl.col("y_coordinate_value").max().alias("max_y"),
        ]
    )

    # Logic to determine gaps:
    # 1. Sort panels by min_x
    # 2. Identify unique X intervals.

    # 1. Distinct sorted X start positions
    x_intervals = panel_bounds.select(["min_x", "max_x"]).unique().sort("min_x")

    # Let's pull to python list for logic clarity
    x_rows = x_intervals.to_dicts()

    current_shift_x = 0.0
    x_shifts_map = {}  # min_x -> shift_amount

    # Helper to capture the compacted width to re-center
    final_max_x = 0.0
    final_min_x = 0.0

    if x_rows:
        prev_max = x_rows[0]["max_x"]
        x_shifts_map[x_rows[0]["min_x"]] = 0.0

        # Calculate shifts to remove gaps
        for i in range(1, len(x_rows)):
            curr_min = x_rows[i]["min_x"]
            curr_max = x_rows[i]["max_x"]

            gap = curr_min - prev_max
            if gap > 0:
                current_shift_x += gap

            x_shifts_map[curr_min] = current_shift_x
            prev_max = max(prev_max, curr_max)

        # Calculate total width after compaction
        # We need to find the global min and max of the shifted intervals.
        # final_max = max(interval_max - shift_of_interval)
        # final_min = min(interval_min - shift_of_interval)

        # Since we have the map, we can iterate rows again or just use the map
        # But shifts are assigned to min_x.
        # A row with min_x gets shift x_shifts_map[min_x].

        # Efficient way:
        current_global_min = float("inf")
        current_global_max = float("-inf")

        for row in x_rows:
            mn = row["min_x"]
            mx = row["max_x"]
            shift = x_shifts_map[mn]

            current_global_min = min(current_global_min, mn - shift)
            current_global_max = max(current_global_max, mx - shift)

        final_max_x = current_global_max
        final_min_x = current_global_min

    # Calculate Center Offset X
    center_x = (final_min_x + final_max_x) / 2.0

    # Update x_shifts_map to include centering
    for k in x_shifts_map:
        x_shifts_map[k] += center_x

    # Same for Y
    y_intervals = panel_bounds.select(["min_y", "max_y"]).unique().sort("min_y")
    y_rows = y_intervals.to_dicts()

    current_shift_y = 0.0
    y_shifts_map = {}

    final_max_y = 0.0
    final_min_y = 0.0

    if y_rows:
        prev_max = y_rows[0]["max_y"]
        y_shifts_map[y_rows[0]["min_y"]] = 0.0

        for i in range(1, len(y_rows)):
            curr_min = y_rows[i]["min_y"]
            curr_max = y_rows[i]["max_y"]

            gap = curr_min - prev_max
            if gap > 0:
                current_shift_y += gap

            y_shifts_map[curr_min] = current_shift_y
            prev_max = max(prev_max, curr_max)

        # Calculate total height after compaction
        current_global_min_y = float("inf")
        current_global_max_y = float("-inf")

        for row in y_rows:
            mn = row["min_y"]
            mx = row["max_y"]
            shift = y_shifts_map[mn]

            current_global_min_y = min(current_global_min_y, mn - shift)
            current_global_max_y = max(current_global_max_y, mx - shift)

        final_max_y = current_global_max_y
        final_min_y = current_global_min_y

    center_y = (final_min_y + final_max_y) / 2.0

    for k in y_shifts_map:
        y_shifts_map[k] += center_y

    # Store gapless bounds for Grid Calculation
    gapless_bounds = {
        "min_x": final_min_x - center_x,
        "max_x": final_max_x - center_x,
        "min_y": final_min_y - center_y,
        "max_y": final_max_y - center_y,
    }

    # Now map these shifts back to panel_bounds so we can join with defects
    # Helper to apply map
    def get_x_shift(val):
        for k, v in x_shifts_map.items():
            if abs(k - val) < 0.01:
                return v
        return 0.0

    def get_y_shift(val):
        for k, v in y_shifts_map.items():
            if abs(k - val) < 0.01:
                return v
        return 0.0

    panel_shifts = {}
    for row in panel_bounds.iter_rows(named=True):
        addr = row["panel_location_info"]
        sx = get_x_shift(row["min_x"])
        sy = get_y_shift(row["min_y"])
        panel_shifts[addr] = (sx, sy)

    return panel_shifts, x_shifts_map, y_shifts_map, gapless_bounds


def apply_gap_removal_and_grid(
    defect_df: pl.DataFrame, panel_shifts: dict, N: int, M: int, gapless_bounds: dict
) -> pl.DataFrame:
    # 1. Apply shifts
    shift_data = [
        {"panel_addr": k, "shift_x": v[0], "shift_y": v[1]}
        for k, v in panel_shifts.items()
    ]
    shift_df = pl.DataFrame(shift_data)

    # Join defects with shifts
    joined_df = defect_df.join(shift_df, on="panel_addr", how="left")

    # Calculate new coordinates
    # gapless_x = def_pnt_x - shift_x
    # gapless_y = def_pnt_y - shift_y
    joined_df = joined_df.with_columns(
        [
            (pl.col("def_pnt_x") - pl.col("shift_x").fill_null(0.0)).alias("gapless_x"),
            (pl.col("def_pnt_y") - pl.col("shift_y").fill_null(0.0)).alias("gapless_y"),
        ]
    )

    # 2. Apply Grid
    # Use bounds from pnl_map, NOT defects
    min_x = gapless_bounds["min_x"]
    max_x = gapless_bounds["max_x"]
    min_y = gapless_bounds["min_y"]
    max_y = gapless_bounds["max_y"]

    width = max_x - min_x
    height = max_y - min_y

    # Avoid division by zero
    if width == 0:
        width = 1
    if height == 0:
        height = 1

    step_x = width / N
    step_y = height / M

    # Calculate Grid Indices
    # Note: We want 0-based index for logic, then map to label.
    # col_idx: 0..(N-1)
    # row_idx: 0..(M-1)

    col_idx_expr = (
        ((pl.col("gapless_x") - min_x) / step_x).floor().cast(pl.Int32)
    ).clip(0, N - 1)

    row_idx_expr = (
        ((pl.col("gapless_y") - min_y) / step_y).floor().cast(pl.Int32)
    ).clip(0, M - 1)

    # Label sequence: 1..9, a..z
    import string

    label_chars = [str(i) for i in range(1, 10)] + list(string.ascii_lowercase)

    def get_grid_label(i):
        if 0 <= i < len(label_chars):
            return label_chars[i]
        return "z"  # Fallback if out of range

    joined_df = joined_df.with_columns(
        [
            col_idx_expr.alias("grid_col_idx"),  # 0-based
            row_idx_expr.alias("grid_row_idx"),  # 0-based
        ]
    )

    # For labels
    # We need to map both col and row indices to the label sequence
    # Since we can't easily use custom python func in expression without map_elements (slow),
    # let's use joins for both col and row labels.

    # Generate label map DataFrame
    # Max size needed is max(N, M)
    max_idx = max(N, M)
    labels = [get_grid_label(i) for i in range(max_idx)]

    label_map_df = pl.DataFrame({"idx": range(max_idx), "label": labels}).with_columns(
        pl.col("idx").cast(pl.Int32)
    )

    # Join for Column Label
    joined_df = joined_df.join(
        label_map_df.select(
            pl.col("idx").alias("grid_col_idx"), pl.col("label").alias("col_label")
        ),
        on="grid_col_idx",
        how="left",
    )

    # Join for Row Label
    joined_df = joined_df.join(
        label_map_df.select(
            pl.col("idx").alias("grid_row_idx"), pl.col("label").alias("row_label")
        ),
        on="grid_row_idx",
        how="left",
    )

    # Construct sub_panel_addr: "{col_label}{row_label}" -> "1a"
    joined_df = joined_df.with_columns(
        (pl.col("col_label") + pl.col("row_label")).alias("sub_panel_addr")
    )

    # 3. Create a dense N x M grid to ensure 0-defect panels exist
    dense_data = []
    for c in range(N):
        c_label = get_grid_label(c)
        for r in range(M):
            r_label = get_grid_label(r)
            dense_data.append({"sub_panel_addr": f"{c_label}{r_label}"})

    dense_df = pl.DataFrame(dense_data)

    # Outer join to ensure every sub_panel_addr exists.
    # We do a right join of joined_df onto dense_df.
    final_df = joined_df.join(dense_df, on="sub_panel_addr", how="right")

    # Fill nulls for def_pnt_x, def_pnt_y if they came from outer join (0-defect panel)
    final_df = final_df.with_columns(
        [
            pl.col("def_pnt_x").fill_null(0.0),
            pl.col("def_pnt_y").fill_null(0.0),
            pl.col("def_size").fill_null(0.0),
            pl.col("n_defect").fill_null(0)
            if "n_defect" in final_df.columns
            else pl.lit(0).alias("n_defect_dummy"),
        ]
    )

    return final_df
