from fastapi import FastAPI, UploadFile, File, Form, HTTPException
from fastapi.responses import FileResponse
import polars as pl
import os
import tempfile
import shutil
from detector import DefectAnalyzer

app = FastAPI(title="LGD Map Pattern Analysis API")


@app.post("/analyze/pattern")
async def analyze_pattern(
    defect_file: UploadFile = File(...), addr_col: str = Form("panel_addr")
):
    with tempfile.TemporaryDirectory() as temp_dir:
        defect_path = os.path.join(temp_dir, "defect.parquet")

        with open(defect_path, "wb") as f:
            shutil.copyfileobj(defect_file.file, f)

        try:
            # 1. Initialize Analyzer & Generate Heatmap
            analyzer = DefectAnalyzer()
            heatmap = analyzer.load_and_preprocess(defect_path, addr_col=addr_col)

            # 2. Detect Patterns
            detections = analyzer.detect_patterns(heatmap)

            # 3. Create Mapping of (x_char, y_char) -> pattern_type
            # We default everything to 'normal'
            mapping_data = []

            # Analyzer provides x_labels and y_labels
            # y corresponds to rows (minr to maxr)
            # x corresponds to cols (minc to maxc)
            for d in detections:
                minr, minc, maxr, maxc = d["bbox"]
                pattern = d["type"]
                for r in range(minr, maxr):
                    for c in range(minc, maxc):
                        if r < len(analyzer.y_labels) and c < len(analyzer.x_labels):
                            mapping_data.append(
                                {
                                    "y_char": analyzer.y_labels[r],
                                    "x_char": analyzer.x_labels[c],
                                    "pattern_type": pattern,
                                }
                            )

            # Create Mapping DataFrame
            if mapping_data:
                map_df = pl.DataFrame(mapping_data).unique(
                    subset=["x_char", "y_char"], keep="first"
                )
            else:
                map_df = pl.DataFrame(
                    {"x_char": [], "y_char": [], "pattern_type": []},
                    schema={
                        "x_char": pl.Utf8,
                        "y_char": pl.Utf8,
                        "pattern_type": pl.Utf8,
                    },
                )

            # 4. Read original parquet
            df = pl.scan_parquet(defect_path)

            # Add extraction cols to join
            df = df.with_columns(
                [
                    pl.col(addr_col).str.slice(0, 1).alias("x_char"),
                    pl.col(addr_col).str.slice(1, 1).alias("y_char"),
                ]
            )

            # Join with mapping
            df = df.join(map_df.lazy(), on=["x_char", "y_char"], how="left")

            # Fill nulls with 'normal', and drop temporary join columns
            df = df.with_columns(pl.col("pattern_type").fill_null("normal")).drop(
                ["x_char", "y_char"]
            )

            # Collect and save
            result_df = df.collect()

            os.makedirs("output", exist_ok=True)

            import uuid

            file_id = str(uuid.uuid4())
            final_path = os.path.join("output", f"pattern_{file_id}.parquet")
            result_df.write_parquet(final_path)

            # Extract panel addresses for each ranked pattern
            for d in detections:
                minr, minc, maxr, maxc = d["bbox"]
                panels = []
                for r in range(minr, maxr):
                    for c in range(minc, maxc):
                        if r < len(analyzer.y_labels) and c < len(analyzer.x_labels):
                            panels.append(
                                f"{analyzer.x_labels[c]}{analyzer.y_labels[r]}"
                            )
                d["panels"] = panels

                # Convert centroid to standard list to ensure JSON serializable
                d["centroid"] = [float(x) for x in d["centroid"]]

            return {
                "ranked_patterns": detections,
                "download_url": f"/download/{file_id}",
            }

        except Exception as e:
            raise HTTPException(
                status_code=500, detail=f"Pattern Analysis Error: {str(e)}"
            )


@app.get("/download/{file_id}")
async def download_parquet(file_id: str):
    file_path = os.path.join("output", f"pattern_{file_id}.parquet")
    if not os.path.exists(file_path):
        raise HTTPException(status_code=404, detail="File not found")
    return FileResponse(
        file_path,
        filename="pattern_analyzed.parquet",
        media_type="application/octet-stream",
    )
