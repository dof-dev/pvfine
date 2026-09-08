package config

import _ "embed"

//go:embed annotations.json
var AnnotationsJSON []byte

//go:embed lists.json
var ListsJSON []byte
