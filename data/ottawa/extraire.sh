#!/bin/bash
#
set -eu

# Check for required tools
check_command() {
  if ! command -v "$1" &> /dev/null; then
    echo "Error: $1 is required but not installed."
    echo "Please install $1 and try again."
    exit 1
  fi
}

check_command curl
check_command python3
check_command grep
check_command sed

# Create Python script for HTML text extraction
cat > extract_text.py << 'EOF'
#!/usr/bin/env python3
import sys
import re
from html.parser import HTMLParser
import html

class HTMLTextExtractor(HTMLParser):
    def __init__(self):
        super().__init__()
        self.result = []
        self.skip = False
        self.current_heading = ""

    def handle_starttag(self, tag, attrs):
        if tag in ('style', 'script', 'head', 'meta', 'noscript'):
            self.skip = True
        elif tag in ('h1', 'h2', 'h3', 'h4', 'h5', 'h6'):
            # Mark headings for special formatting
            self.current_heading = tag
        elif tag == 'br':
            self.result.append("")
        elif tag == 'p':
            # Add paragraph break
            if self.result and self.result[-1] != "":
                self.result.append("")

    def handle_endtag(self, tag):
        if tag in ('style', 'script', 'head', 'meta', 'noscript'):
            self.skip = False
        elif tag in ('h1', 'h2', 'h3', 'h4', 'h5', 'h6'):
            self.current_heading = ""
            # Add extra space after headings
            self.result.append("")
        elif tag == 'p':
            # Add paragraph break
            if self.result and self.result[-1] != "":
                self.result.append("")

    def handle_data(self, data):
        if not self.skip:
            # Decode HTML entities and clean up text
            text = html.unescape(data.strip())
            text = re.sub(r'\s+', ' ', text)
            
            if text:
                # Format headings
                if self.current_heading:
                    if self.result and self.result[-1] != "":
                        self.result.append("")
                    text = text.upper() if self.current_heading == 'h1' else text
                
                self.result.append(text)

    def get_text(self):
        # Clean up the result by removing redundant whitespace
        clean_result = []
        prev_empty = False
        
        for line in self.result:
            current_empty = (line == "")
            
            # Don't add consecutive empty lines
            if not (prev_empty and current_empty):
                clean_result.append(line)
                
            prev_empty = current_empty
            
        return "\n".join(clean_result)

def extract_text_from_html(html_content):
    extractor = HTMLTextExtractor()
    try:
        extractor.feed(html_content)
    except Exception as e:
        # Handle parsing errors gracefully
        return f"Error parsing HTML: {str(e)}\n\nRaw content:\n{html_content[:500]}..."
    return extractor.get_text()

if __name__ == "__main__":
    html = sys.stdin.read()
    print(extract_text_from_html(html))
EOF

chmod +x extract_text.py

# Set the source URL
SOURCE_URL="https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z"

# Set the working directory to the script's location
cd "$(dirname "$0")"

# Create the output directories if they don't exist
mkdir -p content

# Download the webpage content
echo "Downloading content from $SOURCE_URL..."
curl -s "$SOURCE_URL" > ottawa-extract.txt || {
  echo "Error: Failed to download content from $SOURCE_URL"
  exit 1
}

if [ ! -s ottawa-extract.txt ]; then
  echo "Error: Downloaded file is empty"
  exit 1
fi

# Extract links from the file
echo "Extracting links..."
grep -o 'href="[^"]*"' ottawa-extract.txt | \
  grep -i '/en/living-ottawa/laws-licences-and-permits/laws/' | \
  grep -v 'laws-z' | \
  sed -e 's/href="/https:\/\/ottawa.ca/g' -e 's/"//g' > formated-link-ottawa.txt

echo "Found $(wc -l < formated-link-ottawa.txt) links"

# Create a directory for the content files
mkdir -p content

# Process each link
while read -r link; do
  # Extract the filename from the link
  filename=$(echo "$link" | rev | cut -d'/' -f1 | rev)
  
  if [[ -z "$filename" ]]; then
    filename=$(echo "$link" | rev | cut -d'/' -f2 | rev)
  fi
  
  # Add .txt extension
  output_file="content/${filename}.txt"
  
  echo "Processing $link -> $output_file"
  
  # Download and extract the content
  echo "Downloading $link..."
  curl -s -L "$link" | \
    iconv -f ISO-8859-1 -t UTF-8 2>/dev/null || cat | \
    python3 extract_text.py > "$output_file"
    
  # Check if file was created successfully
  if [ -s "$output_file" ]; then
    echo "  Created $output_file ($(wc -l < "$output_file") lines)"
  else
    echo "  ERROR: Failed to extract content from $link"
  fi
  
  # Add a short delay to avoid hammering the server
  sleep 1
done < formated-link-ottawa.txt

echo "All files have been downloaded and processed."
