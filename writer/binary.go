package writer

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"unicode"

	goavro "github.com/linkedin/goavro/v2"
	parquet "github.com/parquet-go/parquet-go"
	"github.com/razeghi71/dq/table"
)

type inferredType struct {
	typ      table.ValueType
	nullable bool
	fields   []inferredField
	elem     *inferredType
	avroName string
}

type inferredField struct {
	name string
	typ  *inferredType
}

const parquetColumnOrderMetadataKey = "dq.column_order"

func writeAvro(w io.Writer, t *table.Table) error {
	return writeAvroWithTypes(w, t, inferTableTypes(t))
}

func writeAvroWithTypes(w io.Writer, t *table.Table, types []*inferredType) error {
	if len(t.Columns) == 0 {
		return fmt.Errorf("Avro output requires at least one column")
	}
	if err := validateAvroFieldNames(t.Columns, types); err != nil {
		return err
	}
	assignAvroRecordNames(t.Columns, types)

	schema, err := avroSchema(t.Columns, types)
	if err != nil {
		return err
	}

	ocfw, err := goavro.NewOCFWriter(goavro.OCFConfig{
		W:      w,
		Schema: schema,
	})
	if err != nil {
		return fmt.Errorf("cannot create Avro writer: %w", err)
	}

	rows := make([]map[string]interface{}, t.NumRows)
	for i := 0; i < t.NumRows; i++ {
		row := make(map[string]interface{}, len(t.Columns))
		for j, col := range t.Columns {
			value, err := avroNullableValue(t.Col(j).Get(i), types[j])
			if err != nil {
				return fmt.Errorf("cannot encode Avro column %q row %d: %w", col, i, err)
			}
			row[col] = value
		}
		rows[i] = row
	}
	if len(rows) == 0 {
		return nil
	}
	if err := ocfw.Append(rows); err != nil {
		return fmt.Errorf("cannot write Avro rows: %w", err)
	}
	return nil
}

func writeParquet(w io.Writer, t *table.Table) error {
	return writeParquetWithTypes(w, t, inferTableTypes(t))
}

func writeParquetWithTypes(w io.Writer, t *table.Table, types []*inferredType) error {
	if len(t.Columns) == 0 {
		return fmt.Errorf("Parquet output requires at least one column")
	}
	rowType := buildParquetRowStruct(t.Columns, types)
	schema := parquet.SchemaOf(reflect.New(rowType).Interface())
	pw := parquet.NewGenericWriter[any](w, schema)
	pw.SetKeyValueMetadata(parquetColumnOrderMetadataKey, strings.Join(t.Columns, ","))

	rows := make([]any, t.NumRows)
	for i := 0; i < t.NumRows; i++ {
		row := reflect.New(rowType).Elem()
		for j := range t.Columns {
			if err := setParquetStructField(row.Field(j), t.Col(j).Get(i), types[j]); err != nil {
				_ = pw.Close()
				return fmt.Errorf("cannot encode Parquet column %q row %d: %w", t.Columns[j], i, err)
			}
		}
		rows[i] = row.Interface()
	}
	if len(rows) > 0 {
		if _, err := pw.Write(rows); err != nil {
			_ = pw.Close()
			return fmt.Errorf("cannot write Parquet rows: %w", err)
		}
	}
	if err := pw.Close(); err != nil {
		return fmt.Errorf("cannot close Parquet writer: %w", err)
	}
	return nil
}

func buildParquetRowStruct(columns []string, types []*inferredType) reflect.Type {
	return parquetStructType(columns, types)
}

func parquetStructType(names []string, types []*inferredType) reflect.Type {
	fields := make([]reflect.StructField, len(names))
	used := map[string]bool{}
	for i, name := range names {
		fields[i] = parquetStructField(name, types[i], uniqueExportedFieldName(name, used))
	}
	return reflect.StructOf(fields)
}

func parquetStructField(name string, typ *inferredType, fieldName string) reflect.StructField {
	tag := fmt.Sprintf(`parquet:"%s"`, name)
	if typ.typ == table.TypeList {
		tag = fmt.Sprintf(`parquet:"%s,list"`, name)
	}
	return reflect.StructField{
		Name: fieldName,
		Type: parquetReflectType(typ),
		Tag:  reflect.StructTag(tag),
	}
}

func uniqueExportedFieldName(name string, used map[string]bool) string {
	base := exportedFieldName(name)
	candidate := base
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s_%d", base, i)
	}
	used[candidate] = true
	return candidate
}

func exportedFieldName(name string) string {
	if name == "" {
		return "Field"
	}
	var b strings.Builder
	for _, r := range name {
		valid := r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if !valid {
			r = '_'
		}
		if b.Len() == 0 {
			if unicode.IsLetter(r) {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteString("Field")
				if unicode.IsDigit(r) {
					b.WriteRune('_')
					b.WriteRune(r)
				}
			}
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "Field"
	}
	return b.String()
}

func parquetReflectType(typ *inferredType) reflect.Type {
	switch typ.typ {
	case table.TypeList:
		return reflect.SliceOf(parquetListElemReflectType(typ.elem))
	case table.TypeRecord:
		base := parquetRecordStructType(typ.fields)
		if typ.nullable {
			return reflect.PointerTo(base)
		}
		return base
	default:
		base := parquetReflectBaseType(typ)
		if typ.nullable {
			return reflect.PointerTo(base)
		}
		return base
	}
}

func parquetReflectBaseType(typ *inferredType) reflect.Type {
	if typ == nil {
		return reflect.TypeOf("")
	}
	switch typ.typ {
	case table.TypeInt:
		return reflect.TypeOf(int64(0))
	case table.TypeFloat:
		return reflect.TypeOf(float64(0))
	case table.TypeString, table.TypeNull:
		return reflect.TypeOf("")
	case table.TypeBool:
		return reflect.TypeOf(false)
	case table.TypeList:
		return reflect.SliceOf(parquetListElemReflectType(typ.elem))
	case table.TypeRecord:
		return parquetRecordStructType(typ.fields)
	default:
		return reflect.TypeOf("")
	}
}

func parquetRecordStructType(fields []inferredField) reflect.Type {
	names := make([]string, len(fields))
	types := make([]*inferredType, len(fields))
	for i, f := range fields {
		names[i] = f.name
		types[i] = f.typ
	}
	return parquetStructType(names, types)
}

func setParquetStructField(field reflect.Value, v table.Value, typ *inferredType) error {
	if typ.nullable && typ.typ != table.TypeList {
		if v.IsNull() {
			return nil
		}
		ptr := reflect.New(field.Type().Elem())
		if err := setParquetValue(ptr.Elem(), v, typ); err != nil {
			return err
		}
		field.Set(ptr)
		return nil
	}
	return setParquetValue(field, v, typ)
}

func setParquetValue(field reflect.Value, v table.Value, typ *inferredType) error {
	if v.IsNull() {
		if field.Kind() == reflect.Slice {
			field.Set(reflect.MakeSlice(field.Type(), 0, 0))
			return nil
		}
		field.Set(reflect.Zero(field.Type()))
		return nil
	}
	switch typ.typ {
	case table.TypeRecord:
		if v.Type != table.TypeRecord {
			return fmt.Errorf("expected record, got %v", v.Type)
		}
		values := recordValues(v)
		for i, f := range typ.fields {
			if err := setParquetStructField(field.Field(i), values[f.name], f.typ); err != nil {
				return fmt.Errorf("%s: %w", f.name, err)
			}
		}
	case table.TypeList:
		if v.Type != table.TypeList {
			return fmt.Errorf("expected list, got %v", v.Type)
		}
		slice := reflect.MakeSlice(field.Type(), len(v.List), len(v.List))
		for i, item := range v.List {
			if err := setParquetListElement(slice.Index(i), item, typ.elem); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		field.Set(slice)
	default:
		return setParquetScalar(field, v, typ)
	}
	return nil
}

func parquetListElemReflectType(elem *inferredType) reflect.Type {
	if elem == nil {
		return reflect.TypeOf("")
	}
	base := parquetReflectBaseType(elem)
	if !elem.nullable {
		return base
	}
	if parquetNullablePrimitive(elem) {
		return parquetNullablePrimitiveWrapperType(base)
	}
	return reflect.PointerTo(base)
}

func parquetNullablePrimitive(typ *inferredType) bool {
	switch typ.typ {
	case table.TypeInt, table.TypeFloat, table.TypeString, table.TypeBool, table.TypeNull:
		return true
	default:
		return false
	}
}

func parquetNullablePrimitiveWrapperType(base reflect.Type) reflect.Type {
	return reflect.StructOf([]reflect.StructField{{
		Name: "Element",
		Type: reflect.PointerTo(base),
		Tag:  `parquet:"element"`,
	}})
}

func isParquetNullablePrimitiveWrapper(t reflect.Type) bool {
	if t.Kind() != reflect.Struct || t.NumField() != 1 {
		return false
	}
	f := t.Field(0)
	return f.Name == "Element" && f.Type.Kind() == reflect.Pointer
}

func setParquetListElement(field reflect.Value, v table.Value, typ *inferredType) error {
	if isParquetNullablePrimitiveWrapper(field.Type()) {
		elemField := field.Field(0)
		if v.IsNull() {
			elemField.Set(reflect.Zero(elemField.Type()))
			return nil
		}
		ptr := reflect.New(elemField.Type().Elem())
		if err := setParquetValue(ptr.Elem(), v, typ); err != nil {
			return err
		}
		elemField.Set(ptr)
		return nil
	}
	if field.Kind() == reflect.Pointer {
		if v.IsNull() {
			return nil
		}
		ptr := reflect.New(field.Type().Elem())
		if err := setParquetValue(ptr.Elem(), v, typ); err != nil {
			return err
		}
		field.Set(ptr)
		return nil
	}
	return setParquetValue(field, v, typ)
}

func setParquetScalar(field reflect.Value, v table.Value, typ *inferredType) error {
	switch typ.typ {
	case table.TypeInt:
		if v.Type != table.TypeInt {
			return fmt.Errorf("expected int, got %v", v.Type)
		}
		field.SetInt(v.Int)
	case table.TypeFloat:
		f, ok := v.AsFloat()
		if !ok {
			return fmt.Errorf("expected float, got %v", v.Type)
		}
		field.SetFloat(f)
	case table.TypeString, table.TypeNull:
		field.SetString(v.AsString())
	case table.TypeBool:
		if v.Type != table.TypeBool {
			return fmt.Errorf("expected bool, got %v", v.Type)
		}
		field.SetBool(v.Bool)
	default:
		field.SetString(v.AsString())
	}
	return nil
}

func inferTableTypes(t *table.Table) []*inferredType {
	types := make([]*inferredType, len(t.Columns))
	for j := range t.Columns {
		var typ *inferredType
		for i := 0; i < t.NumRows; i++ {
			typ = mergeInferred(typ, inferValue(t.Col(j).Get(i)))
		}
		types[j] = finalizeInferred(typ)
	}
	return types
}

func inferValue(v table.Value) *inferredType {
	switch v.Type {
	case table.TypeNull:
		return &inferredType{typ: table.TypeNull, nullable: true}
	case table.TypeInt, table.TypeFloat, table.TypeString, table.TypeBool:
		return &inferredType{typ: v.Type}
	case table.TypeList:
		var elem *inferredType
		for _, e := range v.List {
			elem = mergeInferred(elem, inferValue(e))
		}
		return &inferredType{typ: table.TypeList, elem: elem}
	case table.TypeRecord:
		fields := make([]inferredField, 0, len(v.Fields))
		for _, f := range v.Fields {
			fields = append(fields, inferredField{name: f.Name, typ: inferValue(f.Value)})
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
		return &inferredType{typ: table.TypeRecord, fields: fields}
	default:
		return &inferredType{typ: table.TypeString}
	}
}

func mergeInferred(a, b *inferredType) *inferredType {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.typ == table.TypeNull {
		out := cloneInferred(b)
		out.nullable = true
		return out
	}
	if b.typ == table.TypeNull {
		out := cloneInferred(a)
		out.nullable = true
		return out
	}
	nullable := a.nullable || b.nullable
	if a.typ == b.typ {
		switch a.typ {
		case table.TypeRecord:
			return &inferredType{typ: table.TypeRecord, nullable: nullable, fields: mergeFields(a.fields, b.fields)}
		case table.TypeList:
			return &inferredType{typ: table.TypeList, nullable: nullable, elem: mergeInferred(a.elem, b.elem)}
		default:
			out := cloneInferred(a)
			out.nullable = nullable
			return out
		}
	}
	if (a.typ == table.TypeInt && b.typ == table.TypeFloat) || (a.typ == table.TypeFloat && b.typ == table.TypeInt) {
		return &inferredType{typ: table.TypeFloat, nullable: nullable}
	}
	return &inferredType{typ: table.TypeString, nullable: nullable}
}

func mergeFields(a, b []inferredField) []inferredField {
	out := make([]inferredField, len(a))
	copy(out, a)
	index := make(map[string]int, len(out))
	for i, f := range out {
		index[f.name] = i
	}
	seen := make(map[string]bool, len(b))
	for _, f := range b {
		seen[f.name] = true
		if i, ok := index[f.name]; ok {
			out[i].typ = mergeInferred(out[i].typ, f.typ)
			continue
		}
		f.typ = markNullable(f.typ)
		index[f.name] = len(out)
		out = append(out, f)
	}
	for i := range out {
		if !seen[out[i].name] {
			out[i].typ = markNullable(out[i].typ)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func finalizeInferred(typ *inferredType) *inferredType {
	if typ == nil {
		return &inferredType{typ: table.TypeString}
	}
	if typ.typ == table.TypeNull {
		return &inferredType{typ: table.TypeString, nullable: true}
	}
	switch typ.typ {
	case table.TypeRecord:
		for i := range typ.fields {
			typ.fields[i].typ = finalizeInferred(typ.fields[i].typ)
		}
	case table.TypeList:
		typ.elem = finalizeInferred(typ.elem)
	}
	return typ
}

func cloneInferred(typ *inferredType) *inferredType {
	if typ == nil {
		return nil
	}
	out := &inferredType{
		typ:      typ.typ,
		nullable: typ.nullable,
		elem:     cloneInferred(typ.elem),
		avroName: typ.avroName,
	}
	if len(typ.fields) > 0 {
		out.fields = make([]inferredField, len(typ.fields))
		for i, f := range typ.fields {
			out.fields[i] = inferredField{name: f.name, typ: cloneInferred(f.typ)}
		}
	}
	return out
}

func markNullable(typ *inferredType) *inferredType {
	out := cloneInferred(typ)
	if out == nil {
		return &inferredType{typ: table.TypeString, nullable: true}
	}
	out.nullable = true
	return out
}

func nativeValue(v table.Value, typ *inferredType) (any, error) {
	if v.IsNull() {
		return nil, nil
	}
	switch typ.typ {
	case table.TypeInt:
		if v.Type == table.TypeInt {
			return v.Int, nil
		}
		return nil, fmt.Errorf("expected int, got %v", v.Type)
	case table.TypeFloat:
		if v.Type == table.TypeInt {
			return float64(v.Int), nil
		}
		if v.Type != table.TypeFloat {
			return nil, fmt.Errorf("expected float, got %v", v.Type)
		}
		return v.Float, nil
	case table.TypeString:
		return v.AsString(), nil
	case table.TypeBool:
		if v.Type != table.TypeBool {
			return nil, fmt.Errorf("expected bool, got %v", v.Type)
		}
		return v.Bool, nil
	case table.TypeRecord:
		if v.Type != table.TypeRecord {
			return nil, fmt.Errorf("expected record, got %v", v.Type)
		}
		row := make(map[string]any, len(typ.fields))
		values := recordValues(v)
		for _, f := range typ.fields {
			value, err := nativeValue(values[f.name], f.typ)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.name, err)
			}
			row[f.name] = value
		}
		return row, nil
	case table.TypeList:
		if v.Type != table.TypeList {
			return nil, fmt.Errorf("expected list, got %v", v.Type)
		}
		if typ.elem != nil && typ.elem.nullable {
			return nativeNullableList(v.List, typ.elem)
		}
		if typ.elem != nil {
			if typed, err := nativeList(v.List, typ.elem); err != nil {
				return nil, err
			} else if typed != nil {
				return typed, nil
			}
		}
		items := make([]any, len(v.List))
		for i, item := range v.List {
			value, err := nativeValue(item, typ.elem)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			items[i] = value
		}
		return items, nil
	default:
		return v.AsString(), nil
	}
}

func recordValues(v table.Value) map[string]table.Value {
	values := make(map[string]table.Value, len(v.Fields))
	if v.Type != table.TypeRecord {
		return values
	}
	for _, f := range v.Fields {
		values[f.Name] = f.Value
	}
	return values
}

func avroSchema(columns []string, types []*inferredType) (string, error) {
	fields := make([]map[string]any, len(columns))
	for i, col := range columns {
		fields[i] = avroField(col, types[i])
	}
	schema := map[string]any{
		"type":   "record",
		"name":   "dq_row",
		"fields": fields,
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("cannot build Avro schema: %w", err)
	}
	return string(data), nil
}

func avroField(name string, typ *inferredType) map[string]any {
	field := map[string]any{
		"name": name,
		"type": avroTypeSchema(typ),
	}
	if typ.nullable {
		field["default"] = nil
	}
	return field
}

func avroTypeSchema(typ *inferredType) any {
	if typ.nullable {
		return []any{"null", avroNonNullSchema(typ)}
	}
	return avroNonNullSchema(typ)
}

func avroNonNullSchema(typ *inferredType) any {
	switch typ.typ {
	case table.TypeInt:
		return "long"
	case table.TypeFloat:
		return "double"
	case table.TypeString, table.TypeNull:
		return "string"
	case table.TypeBool:
		return "boolean"
	case table.TypeList:
		return map[string]any{
			"type":  "array",
			"items": avroTypeSchema(typ.elem),
		}
	case table.TypeRecord:
		fields := make([]map[string]any, len(typ.fields))
		for i, f := range typ.fields {
			fields[i] = avroField(f.name, f.typ)
		}
		return map[string]any{
			"type":   "record",
			"name":   typ.avroName,
			"fields": fields,
		}
	default:
		return "string"
	}
}

func avroNullableValue(v table.Value, typ *inferredType) (any, error) {
	if v.IsNull() {
		return nil, nil
	}
	value, err := avroNonNullValue(v, typ)
	if err != nil {
		return nil, err
	}
	if typ.nullable {
		return goavro.Union(avroUnionName(typ), value), nil
	}
	return value, nil
}

func avroNonNullValue(v table.Value, typ *inferredType) (any, error) {
	switch typ.typ {
	case table.TypeInt, table.TypeFloat, table.TypeString, table.TypeBool:
		return nativeValue(v, typ)
	case table.TypeList:
		if v.Type != table.TypeList {
			return nil, fmt.Errorf("expected list, got %v", v.Type)
		}
		items := make([]any, len(v.List))
		for i, item := range v.List {
			value, err := avroNullableValue(item, typ.elem)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			items[i] = value
		}
		return items, nil
	case table.TypeRecord:
		if v.Type != table.TypeRecord {
			return nil, fmt.Errorf("expected record, got %v", v.Type)
		}
		row := make(map[string]any, len(typ.fields))
		values := recordValues(v)
		for _, f := range typ.fields {
			value, err := avroNullableValue(values[f.name], f.typ)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.name, err)
			}
			row[f.name] = value
		}
		return row, nil
	default:
		return v.AsString(), nil
	}
}

func avroUnionName(typ *inferredType) string {
	switch typ.typ {
	case table.TypeInt:
		return "long"
	case table.TypeFloat:
		return "double"
	case table.TypeString, table.TypeNull:
		return "string"
	case table.TypeBool:
		return "boolean"
	case table.TypeList:
		return "array"
	case table.TypeRecord:
		return typ.avroName
	default:
		return "string"
	}
}

func avroPath(name string) string {
	var b strings.Builder
	for _, r := range name {
		if isAvroNameChar(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 || !isAvroNameStart(rune(b.String()[0])) {
		return "field_" + b.String()
	}
	return b.String()
}

func assignAvroRecordNames(columns []string, types []*inferredType) {
	used := map[string]int{}
	for i, typ := range types {
		assignAvroRecordName(typ, avroPath(columns[i]), used)
	}
}

func assignAvroRecordName(typ *inferredType, path string, used map[string]int) {
	if typ == nil {
		return
	}
	switch typ.typ {
	case table.TypeRecord:
		base := "Dq_" + avroPath(path) + "_record"
		used[base]++
		typ.avroName = base
		if used[base] > 1 {
			typ.avroName = fmt.Sprintf("%s_%d", base, used[base])
		}
		for _, f := range typ.fields {
			assignAvroRecordName(f.typ, path+"_"+avroPath(f.name), used)
		}
	case table.TypeList:
		assignAvroRecordName(typ.elem, path+"_item", used)
	}
}

func validateAvroFieldNames(columns []string, types []*inferredType) error {
	for i, col := range columns {
		if !isAvroName(col) {
			return fmt.Errorf("Avro output requires column names to match [A-Za-z_][A-Za-z0-9_]*; got %q", col)
		}
		if err := validateAvroNestedFields(types[i], col); err != nil {
			return err
		}
	}
	return nil
}

func validateAvroNestedFields(typ *inferredType, path string) error {
	switch typ.typ {
	case table.TypeRecord:
		for _, f := range typ.fields {
			if !isAvroName(f.name) {
				return fmt.Errorf("Avro output requires record field names to match [A-Za-z_][A-Za-z0-9_]*; got %q in %s", f.name, path)
			}
			if err := validateAvroNestedFields(f.typ, path+"."+f.name); err != nil {
				return err
			}
		}
	case table.TypeList:
		return validateAvroNestedFields(typ.elem, path+"[]")
	}
	return nil
}

func isAvroName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isAvroNameStart(r) {
				return false
			}
			continue
		}
		if !isAvroNameChar(r) {
			return false
		}
	}
	return true
}

func isAvroNameStart(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_'
}

func isAvroNameChar(r rune) bool {
	return isAvroNameStart(r) || (r >= '0' && r <= '9')
}

func nativeList(items []table.Value, elem *inferredType) (any, error) {
	switch elem.typ {
	case table.TypeInt:
		out := make([]int64, len(items))
		for i, item := range items {
			if item.Type != table.TypeInt {
				return nil, fmt.Errorf("[%d]: expected int, got %v", i, item.Type)
			}
			out[i] = item.Int
		}
		return out, nil
	case table.TypeFloat:
		out := make([]float64, len(items))
		for i, item := range items {
			f, ok := item.AsFloat()
			if !ok {
				return nil, fmt.Errorf("[%d]: expected float, got %v", i, item.Type)
			}
			out[i] = f
		}
		return out, nil
	case table.TypeString:
		out := make([]string, len(items))
		for i, item := range items {
			out[i] = item.AsString()
		}
		return out, nil
	case table.TypeBool:
		out := make([]bool, len(items))
		for i, item := range items {
			if item.Type != table.TypeBool {
				return nil, fmt.Errorf("[%d]: expected bool, got %v", i, item.Type)
			}
			out[i] = item.Bool
		}
		return out, nil
	default:
		return nil, nil
	}
}

func nativeNullableList(items []table.Value, elem *inferredType) (any, error) {
	switch elem.typ {
	case table.TypeInt:
		out := make([]any, len(items))
		for i, item := range items {
			if item.IsNull() {
				out[i] = nil
				continue
			}
			if item.Type != table.TypeInt {
				return nil, fmt.Errorf("[%d]: expected int, got %v", i, item.Type)
			}
			out[i] = item.Int
		}
		return out, nil
	case table.TypeFloat:
		out := make([]*float64, len(items))
		for i, item := range items {
			if item.IsNull() {
				continue
			}
			f, ok := item.AsFloat()
			if !ok {
				return nil, fmt.Errorf("[%d]: expected float, got %v", i, item.Type)
			}
			out[i] = &f
		}
		return out, nil
	case table.TypeString:
		out := make([]*string, len(items))
		for i, item := range items {
			if item.IsNull() {
				continue
			}
			s := item.AsString()
			out[i] = &s
		}
		return out, nil
	case table.TypeBool:
		out := make([]*bool, len(items))
		for i, item := range items {
			if item.IsNull() {
				continue
			}
			if item.Type != table.TypeBool {
				return nil, fmt.Errorf("[%d]: expected bool, got %v", i, item.Type)
			}
			v := item.Bool
			out[i] = &v
		}
		return out, nil
	default:
		out := make([]any, len(items))
		for i, item := range items {
			if item.IsNull() {
				out[i] = nil
				continue
			}
			value, err := nativeValue(item, elem)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = value
		}
		return out, nil
	}
}
