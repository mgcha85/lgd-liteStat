package analysis

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"lgd-litestat/database"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

// SaveTrendChart generate chart and save to file path
// Returns the relative URL path for the image
func SaveTrendChart(results []database.DailyResult, filename string, outputDir string) (string, error) {
	if len(results) == 0 {
		return "", nil
	}

	// Group by GroupType
	seriesMap := make(map[string][]database.DailyResult)
	for _, r := range results {
		seriesMap[r.GroupType] = append(seriesMap[r.GroupType], r)
	}

	var series []chart.Series
	colors := []drawing.Color{
		chart.ColorBlue,
		chart.ColorRed,
		chart.ColorGreen,
		chart.ColorOrange,
	}
	colorIdx := 0

	for name, data := range seriesMap {
		// Sort by Date
		sort.Slice(data, func(i, j int) bool {
			return data[i].WorkDate < data[j].WorkDate
		})

		var xValues []time.Time
		var yValues []float64

		for _, d := range data {
			t, err := time.Parse("2006-01-02", d.WorkDate)
			if err != nil {
				continue
			}
			xValues = append(xValues, t)
			yValues = append(yValues, d.AvgDefects)
		}

		if len(xValues) == 0 {
			continue
		}

		s := chart.TimeSeries{
			Name:    name,
			XValues: xValues,
			YValues: yValues,
			Style: chart.Style{
				StrokeColor: colors[colorIdx%len(colors)],
				StrokeWidth: 2,
			},
		}
		series = append(series, s)
		colorIdx++
	}

	graph := chart.Chart{
		Title: "Daily Defect Trend",
		Background: chart.Style{
			Padding: chart.Box{
				Top:  20,
				Left: 20,
			},
		},
		Series: series,
		XAxis: chart.XAxis{
			Name: "Date",
		},
		YAxis: chart.YAxis{
			Name: "Avg Defects",
		},
	}

	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	fullPath := filepath.Join(outputDir, filename)
	// Ensure dir exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create dir: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if err := graph.Render(chart.PNG, f); err != nil {
		return "", fmt.Errorf("failed to render chart: %w", err)
	}

	return "/api/images/" + filename, nil
}
