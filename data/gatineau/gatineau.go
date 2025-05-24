// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

// Package Gatineau embeds the data pour les règlements de la Ville de Gatineau.
package gatineau

import (
	"embed"
	"io/fs"
)

//go:embed ingested
var dataFS embed.FS

// DataFS contains the pages under ingested/.
var DataFS fs.FS

func init() {
	f, err := fs.Sub(dataFS, "ingested")
	if err != nil {
		panic(err)
	}
	DataFS = f
}
