#!/bin/bash
# Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

set -eu
cd "$(dirname $0)"
cd ./pages_text

rm -f summaries.txt
for file in *.txt; do
	echo "- $file"
	echo "- $file: $(summarize $file)" >> summaries.txt
done

