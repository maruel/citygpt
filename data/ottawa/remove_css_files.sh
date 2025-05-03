#!/bin/bash
# Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

# Get the current directory
CURRENT_DIR=$(pwd)

# Remove all .css files in the current directory
rm "$CURRENT_DIR"/*.css

echo "All .css files removed from $CURRENT_DIR"

