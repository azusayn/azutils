package aigateway

import (
	"encoding/json"
	"testing"
)

func TestResponseMatchesSchema(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["name", "address", "tags"],
		"properties": {
			"name": {"type": "string"},
			"address": {
				"type": "object",
				"additionalProperties": false,
				"required": ["city"],
				"properties": {"city": {"type": "string"}}
			},
			"tags": {
				"type": "array",
				"items": {
					"type": "object",
					"additionalProperties": false,
					"required": ["label"],
					"properties": {"label": {"type": "string"}}
				}
			}
		}
	}`)
	compiled, err := compileResponseSchema(schema)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "exact", content: `{"name":"a","address":{"city":"b"},"tags":[{"label":"c"}]}`, want: true},
		{name: "missing", content: `{"name":"a","address":{"city":"b"}}`, want: false},
		{name: "extra", content: `{"name":"a","address":{"city":"b"},"tags":[],"extra":1}`, want: false},
		{name: "nested extra", content: `{"name":"a","address":{"city":"b","zip":"1"},"tags":[]}`, want: false},
		{name: "nested missing", content: `{"name":"a","address":{},"tags":[]}`, want: false},
		{name: "item extra", content: `{"name":"a","address":{"city":"b"},"tags":[{"label":"c","id":1}]}`, want: false},
		{name: "wrong type", content: `{"name":1,"address":{"city":"b"},"tags":[]}`, want: false},
		{name: "invalid json", content: `{`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := responseMatchesSchema(test.content, compiled)
			if got != test.want {
				t.Fatalf("responseMatchesSchema() = %v, want %v", got, test.want)
			}
		})
	}
}
