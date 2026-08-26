package main

import "embed"

//go:embed all:frontend/dist
var frontendFS embed.FS

//go:embed sql/schema/*.sql
var schemaFS embed.FS

//go:embed data/b3_holidays.json data/contracts.json data/indexes/*.json data/rates/*.json
var dataFS embed.FS
