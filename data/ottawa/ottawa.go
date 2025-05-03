// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package ottawa embeds the data for Ottawa's By-Laws.
package ottawa

import "embed"

//go:embed *
var DataFS embed.FS
