// Package web embeds the browser pages served by the server.
package web

import _ "embed"

// Play is the phone controller page. It must stay a single inline HTML file
// with no external requests, under 10 KB gzipped.
//
//go:embed play.html
var Play []byte

// Host is the big-screen page.
//
//go:embed host.html
var Host []byte
