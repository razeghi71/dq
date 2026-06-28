package loader

import (
	"github.com/razeghi71/dq/table"
)

func avroValue(v any, schema any, namespace string) table.Value {
	return newAvroSchemaContext(schema, namespace).value(v, schema, namespace)
}

func avroSchemaName(schema any, namespace string) string {
	return newAvroSchemaContext(schema, namespace).schemaName(schema, namespace)
}

func avroRecordValue(v any, schema map[string]any, namespace string) table.Value {
	return newAvroSchemaContext(schema, namespace).recordValue(v, schema, namespace)
}

func avroArrayValue(v any, itemSchema any, namespace string) table.Value {
	return newAvroSchemaContext(itemSchema, namespace).arrayValue(v, itemSchema, namespace)
}

func avroFieldSchemaDescriptor(schema any, namespace string) *table.TypeDescriptor {
	return newAvroSchemaContext(schema, namespace).fieldSchemaDescriptor(schema, namespace, nil)
}

func avroRecordSchemaDescriptor(schema map[string]any, namespace string) *table.TypeDescriptor {
	return newAvroSchemaContext(schema, namespace).recordSchemaDescriptor(schema, namespace, nil)
}
