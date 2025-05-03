#!/bin/bash
#
set -eu

# Check for required tools
check_command() {
  if ! command -v "$1" &> /dev/null; then
    echo "Error: $1 is required but not installed."
    if [ "$1" = "lynx" ]; then
      echo "Installing lynx..."
      apt-get update && apt-get install -y lynx || {
        echo "Failed to install lynx automatically. Please install it manually."
        exit 1
      }
    else
      echo "Please install $1 and try again."
      exit 1
    fi
  fi
}

check_command curl
check_command lynx
check_command grep
check_command sed

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
  curl -s "$link" | \
    lynx -dump -nolist -stdin > "$output_file"
  
  # Add a short delay to avoid hammering the server
  sleep 1
done < formated-link-ottawa.txt

echo "All files have been downloaded and processed."
