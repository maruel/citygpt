#!/bin/bash

# Target URL
URL="https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z"

# Output file
OUTPUT="links.txt"

# Fetch HTML and extract links
curl -s "$URL" | grep -oP 'href="\K[^"]+' > "$OUTPUT"

echo "Links extracted to $OUTPUT"

