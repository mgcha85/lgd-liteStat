import polars as pl
import numpy as np
from typing import List, Dict, Any
from skimage import measure, filters


class DefectAnalyzer:
    def __init__(self):
        self.height = 0
        self.width = 0
        self.x_labels = []
        self.y_labels = []

    def load_and_preprocess(
        self, parquet_path: str, addr_col: str = "panel_addr"
    ) -> np.ndarray:
        df = pl.read_parquet(parquet_path)

        parsed_df = df.with_columns(
            [
                pl.col(addr_col).str.slice(0, 1).alias("x_char"),
                pl.col(addr_col).str.slice(1, 1).alias("y_char"),
            ]
        )

        unique_x = (
            parsed_df.select("x_char").unique().sort("x_char")["x_char"].to_list()
        )
        unique_y = (
            parsed_df.select("y_char").unique().sort("y_char")["y_char"].to_list()
        )

        if not unique_x or not unique_y:
            self.x_labels = []
            self.y_labels = []
            return np.array([[]])

        self.x_labels = unique_x
        self.y_labels = unique_y
        self.width = len(unique_x)
        self.height = len(unique_y)

        grid_data = []
        for y_idx, y_char in enumerate(unique_y):
            for x_idx, x_char in enumerate(unique_x):
                grid_data.append(
                    {"x_char": x_char, "y_char": y_char, "x_idx": x_idx, "y_idx": y_idx}
                )
        grid_df = pl.DataFrame(grid_data)

        total_products = df.select("product_id").n_unique()
        agg_df = parsed_df.group_by(["x_char", "y_char"]).agg(
            pl.col("n_defect").sum().alias("total_defects")
        )

        final_df = grid_df.join(
            agg_df, on=["x_char", "y_char"], how="left"
        ).with_columns(pl.col("total_defects").fill_null(0))

        sorted_df = final_df.sort(["y_idx", "x_idx"])

        defects_array = sorted_df.select("total_defects").to_numpy().flatten()

        defect_rate_array = (
            defects_array / total_products
            if total_products > 0
            else defects_array * 0.0
        )

        heatmap = defect_rate_array.reshape((self.height, self.width))

        return heatmap

    def detect_patterns(
        self, heatmap: np.ndarray, threshold_factor: float = 3.0
    ) -> List[Dict[str, Any]]:
        if heatmap.size == 0:
            return []

        smoothed = filters.gaussian(heatmap, sigma=0.5)

        mean_val = np.mean(smoothed)
        std_val = np.std(smoothed)

        if std_val < 1e-6:
            std_val = 1.0

        threshold = mean_val + (std_val * 2.0)

        binary_map = smoothed > threshold

        labeled_map = measure.label(binary_map, connectivity=2)
        regions = measure.regionprops(labeled_map)

        detections = []

        for region in regions:
            minr, minc, maxr, maxc = region.bbox
            h = maxr - minr
            w = maxc - minc
            area = region.area
            extent = region.extent
            solidity = region.solidity

            pattern_type = "unknown"

            if area <= 2:
                pattern_type = "spot"
            elif extent > 0.85:
                if h == 1 or w == 1:
                    if h > w:
                        pattern_type = "vertical_line"
                    else:
                        pattern_type = "horizontal_line"
                elif h > 3 * w:
                    pattern_type = "vertical_line"
                elif w > 3 * h:
                    pattern_type = "horizontal_line"
                else:
                    pattern_type = "rectangle"
            elif solidity < 0.8:
                pattern_type = "donut"
            elif 0.3 < extent < 0.75:
                pattern_type = "triangle"
            else:
                pattern_type = "cluster"

            detections.append(
                {
                    "type": pattern_type,
                    "bbox": (minr, minc, maxr, maxc),
                    "centroid": region.centroid,
                    "confidence": 0.9,
                }
            )

        # Apply Ranking and Conflict Resolution
        return self.rank_patterns(detections)

    def rank_patterns(self, detections: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
        if not detections:
            return []

        # 1. Define Hierarchy Score (Higher is better)
        hierarchy = {
            "vertical_line": 5,
            "horizontal_line": 5,
            "rectangle": 5,
            "donut": 4,
            "cluster": 3,
            "triangle": 2,
            "spot": 1,
            "unknown": 0,
        }

        # 2. Add scoring metrics to each detection
        for d in detections:
            minr, minc, maxr, maxc = d["bbox"]
            area = (maxr - minr) * (maxc - minc)
            d["hierarchy_score"] = hierarchy.get(d["type"], 0)
            d["area"] = area

        # 3. Sort by Hierarchy (Desc) -> Area (Desc)
        sorted_detections = sorted(
            detections, key=lambda x: (x["hierarchy_score"], x["area"]), reverse=True
        )

        # 4. Conflict Resolution (Non-Maxima Suppression)
        # If a lower-ranked pattern heavily overlaps a higher-ranked one, we discard the lower one.
        final_detections = []
        claimed_cells = set()

        for d in sorted_detections:
            minr, minc, maxr, maxc = d["bbox"]

            # Check overlap
            pattern_cells = set()
            for r in range(minr, maxr):
                for c in range(minc, maxc):
                    pattern_cells.add((r, c))

            # If this pattern shares more than 50% of its cells with already claimed higher-rank patterns, drop it
            overlap = len(pattern_cells.intersection(claimed_cells))
            if overlap > 0 and (overlap / len(pattern_cells)) > 0.5:
                continue

            # Keep pattern and claim its cells
            claimed_cells.update(pattern_cells)
            final_detections.append(d)

        # 5. Format output with definitive rank
        for idx, d in enumerate(final_detections):
            d["rank"] = idx + 1
            # Clean up temporary sorting keys
            d.pop("hierarchy_score", None)
            d.pop("area", None)

        return final_detections
