# Ottawa Data Tool

This Go program replaces the original shell script (`extract_links.sh`) and Python script (`download_texts.py`) used to collect data from the Ottawa city website.

## Functionality

The tool can:

1. Extract links from the Ottawa city website's law page
2. Download the content of those links and extract clean text
3. Save the text content to files in a specified output directory

## Usage

```bash
# Run both extraction and download (default behavior)
go run ./cmd/ottawa-data/main.go

# Only extract links without downloading content
go run ./cmd/ottawa-data/main.go -extract-only

# Only download content using an existing links file
go run ./cmd/ottawa-data/main.go -download-only

# Specify custom output directory and links file
go run ./cmd/ottawa-data/main.go -output-dir=custom_dir -links-file=custom_links.txt
```

## Command-line Options

- `-extract-only`: Only extract links without downloading content
- `-download-only`: Only download content using existing links file
- `-output-dir`: Directory to save downloaded text files (default: "pages_text")
- `-links-file`: File to save extracted links (default: "links.txt")

## Building

To build a standalone executable:

```bash
go build -o ottawa-data ./cmd/ottawa-data
```

After building, you can run it directly:

```bash
./ottawa-data
```
