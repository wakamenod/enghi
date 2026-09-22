// Package enghi holds nothing but the embedded templates and static files.
// embed cannot reach into a parent directory, so this lives at the repository
// root while keeping the layout from DESIGN.md (web/templates, web/static).
package enghi

import "embed"

// **NOTE** A directory embed skips files starting with `_` or `.`. Prefixing a
// partial template with _ leaves it silently unembedded and the program dies at
// run time with "no such template", so never name them that way.
//
//go:embed web/templates
var TemplatesFS embed.FS

//go:embed web/static
var StaticFS embed.FS

// GuideFS holds the guide itself (docs/guide/<lang>/<topic>.md).
// **The guide ships inside the binary, as part of the program.** Loading it
// into the database would let the user edit or delete it, and every release
// would collide with those changes.
//
//go:embed docs/guide
var GuideFS embed.FS
