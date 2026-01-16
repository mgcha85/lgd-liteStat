package charting

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"

	"lgd-litestat/database"
)

// Generator handles chart image creation
type Generator struct{}

func NewGenerator() *Generator {
	return &Generator{}
}

// GenerateDailyTrend creates a PNG image for Daily Trend
func (g *Generator) GenerateDailyTrend(results []database.DailyResult) ([]byte, error) {
	// Separate Target vs Others
	targetSeries := chart.TimeSeries{
		Name: "Target",
		Style: chart.Style{
			StrokeColor: drawing.ColorFromHex("e74c3c"),
			StrokeWidth: 2,
		},
	}
	othersSeries := chart.TimeSeries{
		Name: "Others",
		Style: chart.Style{
			StrokeColor: drawing.ColorFromHex("95a5a6"),
			StrokeWidth: 1,
		},
	}

	for _, r := range results {
		t, err := time.Parse("2006-01-02", r.WorkDate)
		if err != nil {
			continue
		}
		if r.GroupType == "Target" {
			targetSeries.XValues = append(targetSeries.XValues, t)
			targetSeries.YValues = append(targetSeries.YValues, r.AvgDefects)
		} else {
			othersSeries.XValues = append(othersSeries.XValues, t)
			othersSeries.YValues = append(othersSeries.YValues, r.AvgDefects)
		}
	}

	graph := chart.Chart{
		Width:  800,
		Height: 400,
		Background: chart.Style{
			Padding: chart.Box{Top: 20, Left: 20, Right: 20, Bottom: 20},
		},
		XAxis: chart.XAxis{
			Name: "Date",
		},
		YAxis: chart.YAxis{
			Name: "Avg Defects",
		},
		Series: []chart.Series{othersSeries, targetSeries},
	}

	buffer := bytes.NewBuffer([]byte{})
	err := graph.Render(chart.PNG, buffer)
	return buffer.Bytes(), err
}

// GenerateHeatmap creates a PNG image for Heatmap
// Note: go-chart doesn't have native Heatmap. We can approximate with Scatter or grid.
// For simplicity, we'll try to find a way or skip if too complex without specialized lib.
// Alternative: Generate simple SVG grid manually which is easy for heatmaps.
func (g *Generator) GenerateHeatmap(cells []database.HeatmapCell) ([]byte, error) {
	// Simple SVG Generator for Heatmap
	width := 600
	height := 600
	padding := 50

	// Collect Coords
	xSet := make(map[string]bool)
	ySet := make(map[string]bool)
	for _, c := range cells {
		xSet[c.X] = true
		ySet[c.Y] = true
	}
	xList := make([]string, 0, len(xSet))
	yList := make([]string, 0, len(ySet))
	for k := range xSet {
		xList = append(xList, k)
	}
	for k := range ySet {
		yList = append(yList, k)
	}
	sort.Strings(xList)
	sort.Strings(yList)

	cols := len(xList)
	rows := len(yList)
	if cols == 0 || rows == 0 {
		return nil, fmt.Errorf("no data for heatmap")
	}

	cellW := (width - 2*padding) / cols
	cellH := (height - 2*padding) / rows

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("<svg width=\"%d\" height=\"%d\" xmlns=\"http://www.w3.org/2000/svg\">", width, height))
	buf.WriteString(fmt.Sprintf("<rect width=\"100%%\" height=\"100%%\" fill=\"white\"/>"))

	// Find Max Defect Rate for scaling
	maxRate := 0.0
	for _, c := range cells {
		if c.DefectRate > maxRate {
			maxRate = c.DefectRate
		}
	}

	for _, c := range cells {
		// Find indices
		xi := -1
		yi := -1
		for i, x := range xList {
			if x == c.X {
				xi = i
				break
			}
		}
		for i, y := range yList {
			if y == c.Y {
				yi = i
				break
			}
		}

		if xi >= 0 && yi >= 0 {
			xPos := padding + xi*cellW
			yPos := padding + yi*cellH

			// Color interpolation (White to Red)
			intensity := 0.0
			if maxRate > 0 {
				intensity = c.DefectRate / maxRate
			}
			// R=255, G=255*(1-int), B=255*(1-int) -> Red scale
			gb := int(255 * (1 - intensity))
			color := fmt.Sprintf("rgb(255,%d,%d)", gb, gb)

			buf.WriteString(fmt.Sprintf("<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" fill=\"%s\" stroke=\"#eee\" />", xPos, yPos, cellW, cellH, color))
			// Text Value? Too small maybe.
		}
	}

	// Draw Axes Labels (Simplified)

	buf.WriteString("</svg>")
	return buf.Bytes(), nil
}

// GenerateScatter creates a PNG image for Glass/Lot Scatter
func (g *Generator) GenerateScatter(targetData, othersData []database.GlassResult) ([]byte, error) {
	// Scatter Plot: X = Index (or Time), Y = TotalDefects
	// Since Glass IDs are categorical/sequence, we can use integer X values.

	targetSeries := chart.ContinuousSeries{
		Name: "Target",
		Style: chart.Style{
			StrokeWidth: chart.Disabled,
			DotWidth:    4,
			DotColor:    drawing.ColorFromHex("e74c3c"),
		},
	}
	othersSeries := chart.ContinuousSeries{
		Name: "Others",
		Style: chart.Style{
			StrokeWidth: chart.Disabled,
			DotWidth:    3,
			DotColor:    drawing.ColorFromHex("95a5a6"),
		},
	}

	// Helper to add points
	maxVal := 0.0
	addPoints := func(data []database.GlassResult, series *chart.ContinuousSeries, startIdx int) {
		for i, d := range data {
			val := float64(d.TotalDefects)
			series.XValues = append(series.XValues, float64(startIdx+i))
			series.YValues = append(series.YValues, val)
			if val > maxVal {
				maxVal = val
			}
		}
	}

	// Add Others first (background), then Target
	addPoints(othersData, &othersSeries, 0)
	addPoints(targetData, &targetSeries, len(othersData)) // Append or Interleave?
	// Actually, if we want to compare distributions, maybe they share X range?
	// Or maybe X is time. Let's try parsing time.
	// Parsing time for scatter is better if available.

	useTime := true
	if len(targetData) > 0 {
		_, err := time.Parse("2006-01-02", targetData[0].WorkDate)
		if err != nil {
			useTime = false
		}
	}

	if useTime {
		// Reset
		targetSeries.XValues = []float64{}
		targetSeries.YValues = []float64{}
		othersSeries.XValues = []float64{}
		othersSeries.YValues = []float64{}

		addTimePoints := func(data []database.GlassResult, series *chart.ContinuousSeries) {
			for _, d := range data {
				t, err := time.Parse("2006-01-02", d.WorkDate)
				if err == nil {
					series.XValues = append(series.XValues, float64(t.Unix()))
					series.YValues = append(series.YValues, float64(d.TotalDefects))
				}
			}
		}
		addTimePoints(othersData, &othersSeries)
		addTimePoints(targetData, &targetSeries)
	}

	graph := chart.Chart{
		Width:  800,
		Height: 400,
		Background: chart.Style{
			Padding: chart.Box{Top: 20, Left: 20, Right: 20, Bottom: 20},
		},
		XAxis: chart.XAxis{
			Name: "Time/Sequence",
			ValueFormatter: func(v interface{}) string {
				if useTime {
					return time.Unix(int64(v.(float64)), 0).Format("01-02")
				}
				return fmt.Sprintf("%.0f", v)
			},
		},
		YAxis: chart.YAxis{
			Name: "Total Defects",
		},
		Series: []chart.Series{othersSeries, targetSeries},
	}

	// Add Legend
	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	buffer := bytes.NewBuffer([]byte{})
	err := graph.Render(chart.PNG, buffer)
	return buffer.Bytes(), err
}
