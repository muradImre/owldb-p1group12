package parser

import (
	"fmt"
	"log/slog"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func SchemaParser(schema string) (*jsonschema.Schema, error) {

	compiler := jsonschema.NewCompiler()

	sch, err := compiler.Compile(schema)
	if err != nil {
		slog.Error("Error parsing arguments", slog.String("schema", schema), slog.Any("error", err))
		return nil, fmt.Errorf("error: %v", err)
	}

	return sch, err
}
