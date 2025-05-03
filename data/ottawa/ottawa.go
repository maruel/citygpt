// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package ottawa embeds the data for Ottawa's By-Laws.
package ottawa

import (
	"embed"
	"io/fs"

	"github.com/maruel/citygpt"
)

//go:embed pages_text
var dataFS embed.FS

// DataFS contains the pages under pages_text/.
var DataFS citygpt.ReadDirFileFS

func init() {
	f, err := fs.Sub(dataFS, "pages_text")
	if err != nil {
		panic(err)
	}
	DataFS = f.(citygpt.ReadDirFileFS)
}
