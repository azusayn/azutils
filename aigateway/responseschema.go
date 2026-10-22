package aigateway

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	jsonSchemaSpec = "schema.json"
)

type compiledSchema struct {
	raw    json.RawMessage
	schema *jsonschema.Schema
}

func compileResponseSchema(raw json.RawMessage) (*compiledSchema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(jsonSchemaSpec, doc); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile(jsonSchemaSpec)
	if err != nil {
		return nil, err
	}
	return &compiledSchema{raw: raw, schema: schema}, nil
}

func responseMatchesSchema(content string, schema *compiledSchema) bool {
	if schema == nil || schema.schema == nil {
		return false
	}
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(content))
	if err != nil {
		return false
	}
	return schema.schema.Validate(value) == nil
}
