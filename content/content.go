// Package content embeds synthetic onboarding content: knowledge base (test
// instructions), team packs, routes, access catalog and teach-back rubrics.
// Nothing here is an Avito regulation — it is test data for the prototype.
package content

import "embed"

//go:embed kb/*.md routes/*.yaml teams/*.yaml teachback/*.yaml access_catalog.yaml
var FS embed.FS
