// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package citygpt includes comment embeds the data for Ottawa's By-Laws.
package citygpt

import "io/fs"

type ReadDirFileFS interface {
	fs.ReadFileFS
	ReadDir(name string) ([]fs.DirEntry, error)
}
