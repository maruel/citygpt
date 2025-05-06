// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

// Package ottawa embeds the data for Ottawa's By-Laws.
package ottawa

import (
	"embed"
	"io/fs"
)

//go:embed pages_text
var dataFS embed.FS

// DataFS contains the pages under pages_text/.
var DataFS fs.FS

func init() {
	f, err := fs.Sub(dataFS, "pages_text")
	if err != nil {
		panic(err)
	}
	DataFS = f
}
