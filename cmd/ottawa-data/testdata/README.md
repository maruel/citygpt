# Test Data

This directory contains test data used by the Ottawa data processing code.

## HTML Test Files

### Ottawa By-Law HTML Files

The HTML files in this directory (`ottawa_*.html`) are snapshots of Ottawa's by-law pages used for testing the HTML text extraction logic in the `ottawa-data` command.

## Updating Test Data

To download a fresh copy of the ATV by-law HTML file and generate its golden file, you can run:

```bash
cd /path/to/citygpt/cmd/ottawa-data/testdata
go run download_test_page.go
```

This will:
1. Download the current HTML content from the ATV by-law page
2. Save it to this directory
3. Process the HTML and generate a .golden file with the extracted text
4. Process any other existing HTML files in the directory and update their golden files
5. Remind you to add the new files to git

Alternatively, you can use go generate:

```bash
cd /path/to/citygpt
go generate ./cmd/ottawa-data/...
```

## Modifying the Download Target

If you need to download a different by-law page, you can edit the URL in `download_test_page.go` and run the generate command again.
