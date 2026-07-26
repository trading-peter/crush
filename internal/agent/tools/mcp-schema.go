package tools

// keywordsByType maps a JSON Schema type to the validation keywords that are
// meaningful only for that type. When a multi-type schema (e.g.
// `type: ["null", "array"]`) is expanded into anyOf, these keywords must move
// into the matching branch rather than stay on the parent, otherwise strict
// providers such as Moonshot reject the schema with HTTP 400:
//
//	"conflicting keywords found in anyOf with parent: keywords (items) are
//	 defined on the parent schema and inside anyOf"
//
// charm.land/fantasy's schema.Normalize converts type-arrays to anyOf but
// drops the original items/properties schemas and leaves the conflicting
// keywords on the parent. We pre-expand MCP tool schemas ourselves so that
// Normalize becomes a no-op for them.
var keywordsByType = map[string]map[string]bool{
	"array": {
		"items":       true,
		"prefixItems": true,
		"contains":    true,
		"minItems":    true,
		"maxItems":    true,
		"uniqueItems": true,
	},
	"object": {
		"properties":           true,
		"additionalProperties": true,
		"required":             true,
		"patternProperties":    true,
		"minProperties":        true,
		"maxProperties":        true,
		"propertyNames":        true,
	},
	"string": {
		"minLength": true,
		"maxLength": true,
		"pattern":   true,
		"format":    true,
	},
	"number": {
		"minimum":          true,
		"maximum":          true,
		"exclusiveMinimum": true,
		"exclusiveMaximum": true,
		"multipleOf":       true,
	},
	"integer": {
		"minimum":          true,
		"maximum":          true,
		"exclusiveMinimum": true,
		"exclusiveMaximum": true,
		"multipleOf":       true,
	},
}

// allTypeSpecificKeywords is the union of all type-specific keyword sets
// above. Used to strip these keywords from the parent node after expansion.
var allTypeSpecificKeywords = func() map[string]bool {
	out := make(map[string]bool)
	for _, keys := range keywordsByType {
		for k := range keys {
			out[k] = true
		}
	}
	return out
}()

// expandNullableSchemas recursively rewrites JSON Schema nodes that use a
// type-array (e.g. `type: ["null", "array"]`) into an equivalent anyOf form
// where each type-specific keyword (items, properties, ...) lives only in the
// branch of the matching type.
//
// The transformation is idempotent for downstream consumers that also expand
// type-arrays (such as charm.land/fantasy's schema.Normalize): after this runs
// there are no type-arrays left for Normalize to mangle.
//
// Single-element type arrays (e.g. `["string"]`) are collapsed to the scalar
// form. Nodes that already carry their own anyOf/oneOf/allOf/$ref are left
// untouched to avoid clobbering intentional composition.
func expandNullableSchemas(node map[string]any) {
	// Recurse into children first so that nested schemas are normalized
	// before we look at this node.
	for _, child := range node {
		switch v := child.(type) {
		case map[string]any:
			expandNullableSchemas(v)
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					expandNullableSchemas(m)
				}
			}
		}
	}

	typeArr, ok := node["type"].([]any)
	if !ok {
		return
	}

	types := make([]string, 0, len(typeArr))
	for _, t := range typeArr {
		if s, ok := t.(string); ok {
			types = append(types, s)
		}
	}
	if len(types) == 0 {
		return
	}

	// Defensive: don't fight with intentional structural composition.
	for _, structural := range []string{"anyOf", "oneOf", "allOf", "$ref"} {
		if _, present := node[structural]; present {
			// Still collapse the type array to a scalar if it's single so the
			// downstream Normalize has nothing to expand, but leave composition
			// keywords alone.
			if len(types) == 1 {
				node["type"] = types[0]
			}
			return
		}
	}

	// Single-element array: collapse to scalar form.
	if len(types) == 1 {
		node["type"] = types[0]
		return
	}

	// Multi-type: build anyOf branches, moving type-specific keywords into
	// the branch of the matching type only.
	branches := make([]any, 0, len(types))
	for _, t := range types {
		branch := map[string]any{"type": t}
		if valid, ok := keywordsByType[t]; ok {
			for key := range valid {
				if val, present := node[key]; present {
					branch[key] = val
				}
			}
		}
		branches = append(branches, branch)
	}

	// Strip type and all type-specific keywords from the parent. Descriptive
	// keywords (description, title, default, ...) stay on the parent where
	// they apply alongside anyOf without conflicting.
	delete(node, "type")
	for key := range node {
		if allTypeSpecificKeywords[key] {
			delete(node, key)
		}
	}
	node["anyOf"] = branches
}
