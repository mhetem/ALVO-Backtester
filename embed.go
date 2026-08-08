package main

import "embed"

//go:embed all:frontend/dist
var frontendFS embed.FS

//go:embed sql/schema/*.sql
var schemaFS embed.FS
