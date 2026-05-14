# Data Visualization Tools for Element Orion

This document describes the new data visualization capabilities added to Element Orion.

## Overview

Element Orion now includes powerful data visualization tools that allow the agent to create charts and graphs from data and send them directly to Discord. The tools support multiple chart types and can work with data provided directly or parsed from CSV/JSON files.

## Available Tools

### 1. create_chart

Creates a chart or graph from data and saves it as an image file.

**Supported chart types:**
- Bar charts
- Line charts
- Scatter plots

**Parameters:**
- `chart_type` (string, required): Type of chart to create ("bar", "line", or "scatter")
- `data` (object, required): Chart data
  - `x_values` (array of numbers, optional): X-axis values
  - `y_values` (array of numbers, required): Y-axis values
  - `labels` (array of strings, optional): Labels for chart categories
- `title` (string, optional): Chart title
- `x_label` (string, optional): X-axis label
- `y_label` (string, optional): Y-axis label
- `output_file` (string, required): Path where the chart image will be saved
- `width` (number, optional): Chart width in inches (default: 6)
- `height` (number, optional): Chart height in inches (default: 4)
- `color_scheme` (string, optional): Color scheme ("blue", "green", "red", "purple", default: "blue")

**Example usage:**
```json
{
  "name": "create_chart",
  "arguments": {
    "chart_type": "bar",
    "data": {
      "x_values": [1, 2, 3, 4],
      "y_values": [10, 25, 30, 15],
      "labels": ["Q1", "Q2", "Q3", "Q4"]
    },
    "title": "Quarterly Sales Report",
    "x_label": "Quarter",
    "y_label": "Sales (in thousands)",
    "output_file": "charts/sales_report.png",
    "width": 8,
    "height": 6,
    "color_scheme": "blue"
  }
}
```

### 2. create_and_send_chart

Creates a chart and sends it directly to Discord in one step.

**Parameters:**
Same as `create_chart` plus:
- `message` (string, optional): Message to send with the chart
- `channel_id` (string, optional): Target Discord channel ID
- `user_id` (string, optional): Discord user ID for direct messages

**Example usage:**
```json
{
  "name": "create_and_send_chart",
  "arguments": {
    "chart_type": "line",
    "data": {
      "x_values": [1, 2, 3, 4, 5],
      "y_values": [2, 4, 1, 5, 3]
    },
    "title": "Stock Price Trend",
    "output_file": "charts/stock_trend.png",
    "message": "Here's the latest stock price trend chart!",
    "color_scheme": "green"
  }
}
```

## Data Parsing Functions

The visualization tools include helper functions for parsing data from files:

### ParseCSVData

Parses data from CSV files:
- Takes a file path and column indices for X and Y values
- Returns arrays of X values, Y values, and labels

### ParseJSONData

Parses data from JSON files:
- Takes a file path and JSON paths for extracting data
- Returns arrays of X values, Y values, and labels

## Example Data Files

### CSV Example (example_data.csv):
```
quarter,sales,profit
Q1,10000,2000
Q2,25000,5000
Q3,30000,7000
Q4,15000,3000
```

### JSON Example (example_data.json):
```json
{
  "data": {
    "quarters": ["Q1", "Q2", "Q3", "Q4"],
    "sales": [10000, 25000, 30000, 15000],
    "profit": [2000, 5000, 7000, 3000]
  }
}
```

## Usage Examples

### Creating a Bar Chart from Direct Data
```
User: Create a bar chart showing quarterly sales
Agent: [Uses create_chart with quarterly sales data]
```

### Creating and Sending a Line Chart
```
User: Can you show me the stock price trend and send it to Discord?
Agent: [Uses create_and_send_chart to create and send the chart]
```

### Using Data from Files
```
User: Create a chart from the sales data in example_data.csv
Agent: [Uses ParseCSVData to read the file, then create_chart to generate the visualization]
```

## Supported Image Formats

Charts can be saved in the following formats:
- PNG (.png)
- SVG (.svg)

The format is determined by the file extension in the `output_file` parameter.

## Customization Options

### Colors
- Blue (default)
- Green
- Red
- Purple

### Dimensions
Charts can be customized with width and height parameters in inches.

### Labels and Titles
All charts support custom titles and axis labels for better clarity.