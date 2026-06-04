package tools

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object"}
	if len(properties) > 0 {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return typedSchema("string", description)
}

func integerSchema(description string) map[string]any {
	return typedSchema("integer", description)
}

func booleanSchema(description string) map[string]any {
	return typedSchema("boolean", description)
}

func stringEnumSchema(values ...string) map[string]any {
	return map[string]any{
		"type": "string",
		"enum": values,
	}
}

func typedSchema(kind, description string) map[string]any {
	schema := map[string]any{"type": kind}
	if description != "" {
		schema["description"] = description
	}
	return schema
}
