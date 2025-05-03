#!/bin/bash

# Get the current directory
CURRENT_DIR=$(pwd)

# Remove all .css files in the current directory
rm "$CURRENT_DIR"/*.css

echo "All .css files removed from $CURRENT_DIR"

