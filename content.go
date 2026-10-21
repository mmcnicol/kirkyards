// Package kirkyards embeds the transcription data and web templates so the
// server is a single self-contained binary.
package kirkyards

import (
	"embed"
	"io/fs"
)

//go:embed data/places.json data/stones
var data embed.FS

//go:embed web
var web embed.FS

// Data holds places.json and stones/*.json.
func Data() fs.FS { return sub(data, "data") }

// Web holds templates/ and static/.
func Web() fs.FS { return sub(web, "web") }

func sub(fsys fs.FS, dir string) fs.FS {
	f, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // dir is a compile-time constant embedded above
	}
	return f
}
