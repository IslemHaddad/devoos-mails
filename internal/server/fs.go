package server

import (
	"io/fs"
)

// mustSub returns the "static" subtree of the embedded filesystem so files are
// served at /static/<name> rather than /static/static/<name>.
func mustSub(embedded fs.FS) fs.FS {
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
