package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Test_expandNullableSchemas_NullArrayWithItems is the regression test for the
// Moonshot 400 bug. agentmem MCP servers advertise parameters like
//
//	"refs": {"type": ["null","array"], "items": {"type": "string"}}
//
// charm.land/fantasy's schema.Normalize turns that into
//
//	{"items": <original>, "anyOf": [{"type":"null"},{"type":"array","items":{}}]}
//
// which Moonshot rejects with "items are defined on the parent schema and
// inside anyOf". expandNullableSchemas must produce a form where the original
// items schema lives only inside the array branch and the parent carries no
// type-specific keywords.
func Test_expandNullableSchemas_NullArrayWithItems(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type":        []any{"null", "array"},
		"items":       map[string]any{"type": "string"},
		"description": "Refs to filter",
	}

	expandNullableSchemas(input)

	// Parent must not carry type or type-specific keywords.
	_, hasType := input["type"]
	require.False(t, hasType, "type should be removed from parent")
	require.NotContains(t, input, "items", "items must move off the parent to avoid anyOf conflict")

	// Parent must keep descriptive keywords.
	require.Equal(t, "Refs to filter", input["description"])

	// anyOf must exist with two branches preserving the original items schema.
	anyOf, ok := input["anyOf"].([]any)
	require.True(t, ok, "anyOf should be produced")
	require.Len(t, anyOf, 2)

	nullBranch, ok := anyOf[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "null", nullBranch["type"])
	require.NotContains(t, nullBranch, "items", "null branch must not carry items")

	arrayBranch, ok := anyOf[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "array", arrayBranch["type"])
	require.Equal(t, map[string]any{"type": "string"}, arrayBranch["items"], "original items schema must be preserved in array branch")
}

// Test_expandNullableSchemas_NullObjectWithProperties covers the symmetric
// case for nullable objects: properties must move into the object branch only.
func Test_expandNullableSchemas_NullObjectWithProperties(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type":        []any{"null", "object"},
		"properties":  map[string]any{"name": map[string]any{"type": "string"}},
		"description": "filter object",
	}

	expandNullableSchemas(input)

	require.NotContains(t, input, "type")
	require.NotContains(t, input, "properties", "properties must move off parent to avoid anyOf conflict")
	require.Equal(t, "filter object", input["description"])

	anyOf, ok := input["anyOf"].([]any)
	require.True(t, ok)
	require.Len(t, anyOf, 2)

	objectBranch, ok := anyOf[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "object", objectBranch["type"])
	require.Equal(t, map[string]any{"name": map[string]any{"type": "string"}}, objectBranch["properties"])
}

// Test_expandNullableSchemas_NestedRecursion verifies that a type-array
// buried inside nested properties is also expanded.
func Test_expandNullableSchemas_NestedRecursion(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"outer": map[string]any{
				"type":  []any{"null", "array"},
				"items": map[string]any{"type": "string"},
			},
		},
	}

	expandNullableSchemas(input)

	outer := input["properties"].(map[string]any)["outer"].(map[string]any)
	require.NotContains(t, outer, "type")
	require.NotContains(t, outer, "items")
	anyOf, ok := outer["anyOf"].([]any)
	require.True(t, ok)
	require.Len(t, anyOf, 2)
}

// Test_expandNullableSchemas_SingleElementCollapses verifies that
// single-element type arrays like ["string"] are collapsed to scalar form
// rather than wrapped in anyOf.
func Test_expandNullableSchemas_SingleElementCollapses(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type": []any{"string"},
	}

	expandNullableSchemas(input)

	require.Equal(t, "string", input["type"])
	require.NotContains(t, input, "anyOf")
}

// Test_expandNullableSchemas_ScalarTypeIsNoOp verifies that a scalar type
// (plain string) is left untouched.
func Test_expandNullableSchemas_ScalarTypeIsNoOp(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type":  "string",
		"items": map[string]any{"type": "string"},
	}

	expandNullableSchemas(input)

	require.Equal(t, "string", input["type"])
	require.Equal(t, map[string]any{"type": "string"}, input["items"])
}

// Test_expandNullableSchemas_RespectsExistingComposition verifies that nodes
// which already carry anyOf/oneOf/allOf/$ref are left alone (we don't fight
// with intentional structural composition), but a single-element type array
// is still collapsed to scalar.
func Test_expandNullableSchemas_RespectsExistingComposition(t *testing.T) {
	t.Parallel()

	t.Run("anyOf present, multi-type left alone", func(t *testing.T) {
		t.Parallel()
		input := map[string]any{
			"type":  []any{"null", "array"},
			"items": map[string]any{"type": "string"},
			"anyOf": []any{map[string]any{"type": "string"}},
		}
		expandNullableSchemas(input)
		// Should not clobber the existing anyOf.
		originalAnyOf, ok := input["anyOf"].([]any)
		require.True(t, ok)
		require.Len(t, originalAnyOf, 1)
		// Multi-type left in place (we don't try to merge).
		require.Contains(t, input, "type")
	})

	t.Run("$ref present, single-element type still collapsed", func(t *testing.T) {
		t.Parallel()
		input := map[string]any{
			"type": []any{"string"},
			"$ref": "#/$defs/Foo",
		}
		expandNullableSchemas(input)
		require.Equal(t, "string", input["type"])
		require.Equal(t, "#/$defs/Foo", input["$ref"])
	})
}

// Test_Tool_Info_NullableSchemaFix is the end-to-end regression test: build a
// real MCP tool with the agentmem-style nullable-array schema, run it through
// Tool.Info(), and assert the produced schema has no parent/branch keyword
// conflicts that would trigger Moonshot 400.
func Test_Tool_Info_NullableSchemaFix(t *testing.T) {
	t.Parallel()

	agentmemTool := &mcp.Tool{
		Name:        "search",
		Description: "search across the corpus",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"refs": map[string]any{
					"type":        []any{"null", "array"},
					"items":       map[string]any{"type": "string"},
					"description": "Refs to filter",
				},
				"filters": map[string]any{
					"type":        []any{"null", "object"},
					"properties":  map[string]any{"tag": map[string]any{"type": "string"}},
					"description": "Optional filters",
				},
			},
			"required": []any{"query"},
		},
	}

	tool := &Tool{mcpName: "agentmem", tool: agentmemTool}
	info := tool.Info()

	props, ok := info.Parameters["refs"].(map[string]any)
	require.True(t, ok, "refs property should be present")
	require.NotContains(t, props, "type", "parent should not carry type after expansion")
	require.NotContains(t, props, "items", "parent should not carry items after expansion")
	require.Contains(t, props, "description")
	anyOf, ok := props["anyOf"].([]any)
	require.True(t, ok, "refs should be expanded to anyOf")
	require.Len(t, anyOf, 2)
	arrayBranch, ok := anyOf[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "array", arrayBranch["type"])
	require.Equal(t, map[string]any{"type": "string"}, arrayBranch["items"], "original items schema must survive")

	// Same checks for the nullable object property.
	filters, ok := info.Parameters["filters"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, filters, "properties")
	anyOf, ok = filters["anyOf"].([]any)
	require.True(t, ok)
	require.Len(t, anyOf, 2)

	// Required should still be parsed correctly.
	require.Equal(t, []string{"query"}, info.Required)
}
