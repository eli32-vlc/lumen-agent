package tools

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

// ChartData represents the data structure for creating charts
type ChartData struct {
	XValues []float64 `json:"x_values,omitempty"`
	YValues []float64 `json:"y_values,omitempty"`
	Labels  []string  `json:"labels,omitempty"`
}

// ChartConfig represents the configuration for creating charts
type ChartConfig struct {
	ChartType   string    `json:"chart_type"`          // bar, line, scatter
	Data        ChartData `json:"data"`               // chart data
	Title       string    `json:"title,omitempty"`    // chart title
	XLabel      string    `json:"x_label,omitempty"`  // x-axis label
	YLabel      string    `json:"y_label,omitempty"`  // y-axis label
	OutputFile  string    `json:"output_file"`        // output file path
	Width       float64   `json:"width,omitempty"`    // chart width in inches
	Height      float64   `json:"height,omitempty"`   // chart height in inches
	ColorScheme string    `json:"color_scheme,omitempty"` // color scheme for the chart
}

func (r *Registry) registerVisualizationTools() {
	r.register(
		"create_chart",
		"Create a chart or graph from data and save it as an image file. Supported chart types: bar, line, scatter.",
		objectSchema(map[string]any{
			"chart_type": stringSchema("Type of chart to create: bar, line, or scatter."),
			"data": objectSchema(map[string]any{
				"x_values": objectSchema(map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "number"},
					"description": "X-axis values for the chart (required for bar, line and scatter charts).",
				}),
				"y_values": objectSchema(map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "number"},
					"description": "Y-axis values for the chart (required for all chart types).",
				}),
				"labels": objectSchema(map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
					"description": "Labels for bar chart categories.",
				}),
			}),
			"title":         stringSchema("Optional title for the chart."),
			"x_label":       stringSchema("Optional label for the x-axis."),
			"y_label":       stringSchema("Optional label for the y-axis."),
			"output_file":   stringSchema("Path where the chart image will be saved."),
			"width":         integerSchema("Optional width of the chart in inches (default: 6).", 1),
			"height":        integerSchema("Optional height of the chart in inches (default: 4).", 1),
			"color_scheme":  stringSchema("Optional color scheme: blue, green, red, purple (default: blue)."),
		}, "chart_type", "data", "output_file"),
		r.handleCreateChart,
	)
	
	r.register(
		"create_and_send_chart",
		"Create a chart or graph from data, save it as an image file, and send it to Discord.",
		objectSchema(map[string]any{
			"chart_type": stringSchema("Type of chart to create: bar, line, or scatter."),
			"data": objectSchema(map[string]any{
				"x_values": objectSchema(map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "number"},
					"description": "X-axis values for the chart (required for bar, line and scatter charts).",
				}),
				"y_values": objectSchema(map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "number"},
					"description": "Y-axis values for the chart (required for all chart types).",
				}),
				"labels": objectSchema(map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
					"description": "Labels for bar chart categories.",
				}),
			}),
			"title":         stringSchema("Optional title for the chart."),
			"x_label":       stringSchema("Optional label for the x-axis."),
			"y_label":       stringSchema("Optional label for the y-axis."),
			"output_file":   stringSchema("Path where the chart image will be saved."),
			"width":         integerSchema("Optional width of the chart in inches (default: 6).", 1),
			"height":        integerSchema("Optional height of the chart in inches (default: 4).", 1),
			"color_scheme":  stringSchema("Optional color scheme: blue, green, red, purple (default: blue)."),
			"message":       stringSchema("Optional message to send with the chart in Discord."),
			"channel_id":    stringSchema("Optional target Discord channel ID."),
			"user_id":       stringSchema("Optional Discord user ID to DM. Requires allowlist access."),
		}, "chart_type", "data", "output_file"),
		r.handleCreateAndSendChart,
	)
}

func (r *Registry) handleCreateChart(ctx context.Context, payload json.RawMessage) (string, error) {
	var config ChartConfig
	if err := decodeArgs(payload, &config); err != nil {
		return "", err
	}

	// Validate chart type
	config.ChartType = strings.ToLower(strings.TrimSpace(config.ChartType))
	switch config.ChartType {
	case "bar", "line", "scatter":
		// Valid chart types
	default:
		return "", fmt.Errorf("unsupported chart type: %s. Supported types: bar, line, scatter", config.ChartType)
	}

	// Validate data
	if err := r.validateChartData(config); err != nil {
		return "", err
	}

	// Set default dimensions if not provided
	if config.Width <= 0 {
		config.Width = 6
	}
	if config.Height <= 0 {
		config.Height = 4
	}

	// Resolve output file path
	outputPath, err := r.resolvePath(config.OutputFile)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathAccessible(outputPath); err != nil {
		return "", err
	}

	// Create chart directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", fmt.Errorf("create chart directory: %w", err)
	}

	// Create the chart
	if err := r.createChart(config, outputPath); err != nil {
		return "", err
	}

	// Return success message
	return jsonResult(map[string]any{
		"chart_type":  config.ChartType,
		"output_file": r.relPath(outputPath),
		"width":       config.Width,
		"height":      config.Height,
		"title":       config.Title,
	})
}

func (r *Registry) handleCreateAndSendChart(ctx context.Context, payload json.RawMessage) (string, error) {
	// First create the chart using existing logic
	chartResult, err := r.handleCreateChart(ctx, payload)
	if err != nil {
		return "", err
	}
	
	// Parse the output file path from the chart result
	var chartResponse map[string]interface{}
	if err := json.Unmarshal([]byte(chartResult), &chartResponse); err != nil {
		return "", fmt.Errorf("parse chart result: %w", err)
	}
	
	outputFile, ok := chartResponse["output_file"].(string)
	if !ok {
		return "", fmt.Errorf("could not get output file path from chart result")
	}
	
	// Extract message and Discord target information from payload
	type sendArgs struct {
		Message    string `json:"message"`
		ChannelID  string `json:"channel_id"`
		UserID     string `json:"user_id"`
	}
	
	var sendConfig sendArgs
	if err := decodeArgs(payload, &sendConfig); err != nil {
		return "", err
	}
	
	// Now send the file to Discord
	// We need to construct the payload for send_discord_file
	sendPayload := map[string]interface{}{
		"path":       outputFile,
		"message":    sendConfig.Message,
		"channel_id": sendConfig.ChannelID,
		"user_id":    sendConfig.UserID,
	}
	
	sendPayloadBytes, err := json.Marshal(sendPayload)
	if err != nil {
		return "", fmt.Errorf("marshal send payload: %w", err)
	}
	
	discordResult, err := r.handleSendDiscordFile(ctx, json.RawMessage(sendPayloadBytes))
	if err != nil {
		return "", fmt.Errorf("send chart to Discord: %w", err)
	}
	
	// Return combined result
	return jsonResult(map[string]any{
		"chart":           json.RawMessage(chartResult),
		"discord_message": json.RawMessage(discordResult),
	})
}

func (r *Registry) validateChartData(config ChartConfig) error {
	data := config.Data
	
	switch config.ChartType {
	case "bar", "line", "scatter":
		if len(data.YValues) == 0 {
			return fmt.Errorf("y_values is required for %s charts", config.ChartType)
		}
		if len(data.XValues) > 0 && len(data.XValues) != len(data.YValues) {
			return fmt.Errorf("x_values and y_values must have the same length")
		}
	}
	
	return nil
}

func (r *Registry) createChart(config ChartConfig, outputPath string) error {
	// Create a new plot
	p := plot.New()
	
	// Set chart title
	if config.Title != "" {
		p.Title.Text = config.Title
	}
	
	// Set axis labels
	if config.XLabel != "" {
		p.X.Label.Text = config.XLabel
	}
	if config.YLabel != "" {
		p.Y.Label.Text = config.YLabel
	}
	
	// Create the appropriate chart type
	switch config.ChartType {
	case "bar":
		if err := r.createBarChart(p, config); err != nil {
			return err
		}
	case "line":
		if err := r.createLineChart(p, config); err != nil {
			return err
		}
	case "scatter":
		if err := r.createScatterChart(p, config); err != nil {
			return err
		}
	}
	
	// Save the chart
	width := vg.Length(config.Width) * vg.Inch
	height := vg.Length(config.Height) * vg.Inch
	
	// Determine file extension to choose the correct canvas
	ext := strings.ToLower(filepath.Ext(outputPath))
	switch ext {
	case ".png":
		return p.Save(width, height, outputPath)
	case ".svg":
		return p.Save(width, height, outputPath)
	default:
		// Default to PNG
		return p.Save(width, height, outputPath)
	}
}

func (r *Registry) createBarChart(p *plot.Plot, config ChartConfig) error {
	data := config.Data
	
	// Prepare data for plotting
	var values plotter.Values
	if len(data.XValues) > 0 {
		// If X values are provided, use them as indices
		values = make(plotter.Values, len(data.YValues))
		copy(values, data.YValues)
	} else {
		// If no X values, use indices
		values = make(plotter.Values, len(data.YValues))
		copy(values, data.YValues)
	}
	
	// Create bar chart
	bars, err := plotter.NewBarChart(values, vg.Points(20))
	if err != nil {
		return fmt.Errorf("create bar chart: %w", err)
	}
	
	// Set bar color
	bars.Color = r.getColor(config.ColorScheme)
	bars.LineStyle.Width = vg.Length(0)
	bars.Offset = 0
	
	// Add bars to plot
	p.Add(bars)
	
	// Add labels if provided
	if len(data.Labels) > 0 && len(data.Labels) == len(data.YValues) {
		// Create a custom struct that implements XYLabeller
		labelData := &labelPoints{
			XYs:    make(plotter.XYs, len(data.Labels)),
			Labels: data.Labels,
		}
		
		// Position labels at the center of each bar
		for i := range labelData.XYs {
			labelData.XYs[i].X = float64(i)
			labelData.XYs[i].Y = data.YValues[i]
		}
		
		labels, err := plotter.NewLabels(labelData)
		if err != nil {
			return fmt.Errorf("create labels: %w", err)
		}
		
		p.Add(labels)
		p.NominalX(data.Labels...)
	} else if len(data.XValues) > 0 {
		// Use numeric X values as tick marks
		ticks := make([]plot.Tick, len(data.XValues))
		for i, xv := range data.XValues {
			ticks[i] = plot.Tick{Value: xv, Label: fmt.Sprintf("%.1f", xv)}
		}
		p.X.Tick.Marker = plot.ConstantTicks(ticks)
	} else {
		// Use indices as tick marks
		ticks := make([]plot.Tick, len(data.YValues))
		for i := range ticks {
			ticks[i] = plot.Tick{Value: float64(i), Label: fmt.Sprintf("%d", i)}
		}
		p.X.Tick.Marker = plot.ConstantTicks(ticks)
	}
	
	return nil
}

// labelPoints implements the XYLabeller interface for creating labeled points
type labelPoints struct {
	XYs    plotter.XYs
	Labels []string
}

func (lp *labelPoints) Len() int { return len(lp.XYs) }
func (lp *labelPoints) XY(i int) (float64, float64) { return lp.XYs[i].X, lp.XYs[i].Y }
func (lp *labelPoints) Label(i int) string { return lp.Labels[i] }

func (r *Registry) createLineChart(p *plot.Plot, config ChartConfig) error {
	data := config.Data
	
	// Prepare XY data
	var pts plotter.XYs
	if len(data.XValues) > 0 {
		pts = make(plotter.XYs, len(data.YValues))
		for i := range pts {
			pts[i].X = data.XValues[i]
			pts[i].Y = data.YValues[i]
		}
	} else {
		// If no X values, use indices
		pts = make(plotter.XYs, len(data.YValues))
		for i := range pts {
			pts[i].X = float64(i)
			pts[i].Y = data.YValues[i]
		}
	}
	
	// Create line
	line, err := plotter.NewLine(pts)
	if err != nil {
		return fmt.Errorf("create line chart: %w", err)
	}
	
	// Set line color and width
	line.Color = r.getColor(config.ColorScheme)
	line.Width = vg.Points(2)
	
	// Add line to plot
	p.Add(line)
	
	return nil
}

func (r *Registry) createScatterChart(p *plot.Plot, config ChartConfig) error {
	data := config.Data
	
	// Prepare XY data
	var pts plotter.XYs
	if len(data.XValues) > 0 {
		pts = make(plotter.XYs, len(data.YValues))
		for i := range pts {
			pts[i].X = data.XValues[i]
			pts[i].Y = data.YValues[i]
		}
	} else {
		// If no X values, use indices
		pts = make(plotter.XYs, len(data.YValues))
		for i := range pts {
			pts[i].X = float64(i)
			pts[i].Y = data.YValues[i]
		}
	}
	
	// Create scatter
	s, err := plotter.NewScatter(pts)
	if err != nil {
		return fmt.Errorf("create scatter chart: %w", err)
	}
	
	// Set scatter color and glyph style
	s.Color = r.getColor(config.ColorScheme)
	s.GlyphStyle.Radius = vg.Points(3)
	s.GlyphStyle.Shape = draw.CircleGlyph{}
	
	// Add scatter to plot
	p.Add(s)
	
	return nil
}

func (r *Registry) getColor(scheme string) color.Color {
	switch strings.ToLower(scheme) {
	case "red":
		return color.RGBA{255, 0, 0, 255}
	case "green":
		return color.RGBA{0, 255, 0, 255}
	case "purple":
		return color.RGBA{128, 0, 128, 255}
	default: // blue
		return color.RGBA{0, 0, 255, 255}
	}
}

// ParseCSVData reads CSV data from a file and returns X and Y values
func (r *Registry) ParseCSVData(filePath string, xColumn, yColumn int) ([]float64, []float64, []string, error) {
	path, err := r.resolvePath(filePath)
	if err != nil {
		return nil, nil, nil, err
	}
	
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open csv file: %w", err)
	}
	defer file.Close()
	
	reader := csv.NewReader(file)
	
	// Read header (but we don't use it for now)
	_, err = reader.Read()
	if err != nil && err != io.EOF {
		return nil, nil, nil, fmt.Errorf("read csv header: %w", err)
	}
	
	var xValues []float64
	var yValues []float64
	var labels []string
	
	// Read data rows
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read csv row: %w", err)
		}
		
		if len(record) <= xColumn || len(record) <= yColumn {
			continue // Skip rows with insufficient columns
		}
		
		// Parse X value
		if xColumn >= 0 {
			if xVal, err := strconv.ParseFloat(record[xColumn], 64); err == nil {
				xValues = append(xValues, xVal)
			} else {
				// If it's not a number, use it as a label
				labels = append(labels, record[xColumn])
			}
		}
		
		// Parse Y value
		if yVal, err := strconv.ParseFloat(record[yColumn], 64); err == nil {
			yValues = append(yValues, yVal)
		}
	}
	
	return xValues, yValues, labels, nil
}

// ParseJSONData reads JSON data from a file and extracts chart data
func (r *Registry) ParseJSONData(filePath string, xPath, yPath, labelPath string) ([]float64, []float64, []string, error) {
	path, err := r.resolvePath(filePath)
	if err != nil {
		return nil, nil, nil, err
	}
	
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read json file: %w", err)
	}
	
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		// Try to parse as array
		var jsonArray []interface{}
		if err := json.Unmarshal(data, &jsonArray); err != nil {
			return nil, nil, nil, fmt.Errorf("parse json data: %w", err)
		}
		jsonData = map[string]interface{}{"data": jsonArray}
	}
	
	// Extract data based on paths (simplified implementation)
	// In a real implementation, you would need a proper JSONPath implementation
	xValues, err := r.extractFloatArray(jsonData, xPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("extract x values: %w", err)
	}
	
	yValues, err := r.extractFloatArray(jsonData, yPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("extract y values: %w", err)
	}
	
	labels, err := r.extractStringArray(jsonData, labelPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("extract labels: %w", err)
	}
	
	return xValues, yValues, labels, nil
}

func (r *Registry) extractFloatArray(data map[string]interface{}, path string) ([]float64, error) {
	// Simplified implementation - in reality, you'd want proper JSONPath support
	parts := strings.Split(path, ".")
	current := interface{}(data)
	
	for _, part := range parts {
		if mapData, ok := current.(map[string]interface{}); ok {
			current = mapData[part]
		} else {
			return nil, fmt.Errorf("invalid path: %s", path)
		}
	}
	
	if arrayData, ok := current.([]interface{}); ok {
		result := make([]float64, len(arrayData))
		for i, item := range arrayData {
			if num, ok := item.(float64); ok {
				result[i] = num
			} else if str, ok := item.(string); ok {
				if num, err := strconv.ParseFloat(str, 64); err == nil {
					result[i] = num
				}
			}
		}
		return result, nil
	}
	
	return nil, fmt.Errorf("path does not point to an array: %s", path)
}

func (r *Registry) extractStringArray(data map[string]interface{}, path string) ([]string, error) {
	// Simplified implementation - in reality, you'd want proper JSONPath support
	parts := strings.Split(path, ".")
	current := interface{}(data)
	
	for _, part := range parts {
		if mapData, ok := current.(map[string]interface{}); ok {
			current = mapData[part]
		} else {
			return nil, fmt.Errorf("invalid path: %s", path)
		}
	}
	
	if arrayData, ok := current.([]interface{}); ok {
		result := make([]string, len(arrayData))
		for i, item := range arrayData {
			if str, ok := item.(string); ok {
				result[i] = str
			} else {
				result[i] = fmt.Sprintf("%v", item)
			}
		}
		return result, nil
	}
	
	return nil, fmt.Errorf("path does not point to an array: %s", path)
}