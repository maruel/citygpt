#!/bin/bash

for file in pages_text/*.txt; do
  summarize "$file"
done

