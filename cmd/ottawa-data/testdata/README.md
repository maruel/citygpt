# Test Data

This directory contains test data used by the Ottawa data processing code.

## HTML Test Files

### Ottawa By-Law HTML Files

The HTML files in this directory (`ottawa_*.html`) are snapshots of Ottawa's by-law pages used for testing the HTML text extraction logic in the `ottawa-data` command.

## Updating Test Data

To download a fresh copy of the ATV by-law HTML file, you can run:

```bash
cd /path/to/citygpt
go generate ./cmd/ottawa-data/...
```

The HTML files are saved in the `/app/cmd/ottawa-data/testdata` directory (in the container) or the equivalent directory on your local machine.

This will:
1. Download the current HTML content from the ATV by-law page
2. Save it to this directory with a timestamp in the filename
3. Remind you to add the new file to git

## Modifying the Download Target

If you need to download a different by-law page, you can edit the URL in `download_test_page.go` and run the generate command again.
