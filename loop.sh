#!/bin/bash
# Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

set -eu

# Handle Ctrl+C (SIGINT) gracefully
trap "echo -e '\nExiting...'; exit 0" INT

while true; do
  citygpt "$@"
done
