#!/bin/bash
# Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

# Target URL
URL="https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z"

# Output file
OUTPUT="links.txt"

# Fetch HTML and extract links
curl -s "$URL" | grep -oP 'href="\K[^"]+' > "$OUTPUT"

echo "Links extracted to $OUTPUT"

