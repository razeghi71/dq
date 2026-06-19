package table

import "testing"

func TestFixedSchemaContractAddRowTypedScalarCompatibility(t *testing.T) {
	tbl := NewTableWithSchemas([]string{"id", "amount"}, []*TypeDescriptor{
		td(TypeInt),
		td(TypeFloat),
	})

	if err := tbl.AddRowTyped([]Value{IntVal(1), IntVal(10)}); err != nil {
		t.Fatalf("AddRowTyped int/int returned error: %v", err)
	}
	if got := tbl.Get(0, "amount"); got.Type != TypeFloat || got.Float != 10 {
		t.Fatalf("int value was not promoted into planned float column: got %v", got)
	}
	requireSchemaString(t, tbl.Col(tbl.ColIndex("id")).Schema(), "int")
	requireSchemaString(t, tbl.Col(tbl.ColIndex("amount")).Schema(), "float")
}

func TestFixedSchemaContractAddRowTypedRejectsScalarStringification(t *testing.T) {
	cases := []struct {
		name   string
		schema *TypeDescriptor
		value  Value
		path   string
		want   string
		got    string
	}{
		{
			name:   "string_into_int",
			schema: td(TypeInt),
			value:  StrVal("42"),
			path:   "v",
			want:   "int",
			got:    "string",
		},
		{
			name:   "bool_into_string",
			schema: td(TypeString),
			value:  BoolVal(true),
			path:   "v",
			want:   "string",
			got:    "bool",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := NewTableWithSchemas([]string{"v"}, []*TypeDescriptor{tc.schema})
			err := tbl.AddRowTyped([]Value{tc.value})
			requireSchemaError(t, err, tc.path, tc.want, tc.got)
			if tbl.NumRows != 0 || tbl.Col(0).Len() != 0 {
				t.Fatalf("failed strict append partially modified table: rows=%d len=%d", tbl.NumRows, tbl.Col(0).Len())
			}
		})
	}
}

func TestFixedSchemaContractAddRowTypedNestedValidationAndMissingFields(t *testing.T) {
	schema := recordOf(
		field("id", td(TypeInt)),
		field("meta", recordOf(field("score", td(TypeFloat)), field("tag", WithNullable(td(TypeString))))),
	)
	tbl := NewTableWithSchemas([]string{"payload"}, []*TypeDescriptor{schema})

	if err := tbl.AddRowTyped([]Value{
		RecordVal([]RecordField{
			{Name: "id", Value: IntVal(1)},
			{Name: "meta", Value: RecordVal([]RecordField{{Name: "score", Value: IntVal(9)}})},
		}),
	}); err != nil {
		t.Fatalf("AddRowTyped nested record returned error: %v", err)
	}

	payload := tbl.Get(0, "payload")
	fields := recordValuesForFixedSchemaContract(payload)
	meta := recordValuesForFixedSchemaContract(fields["meta"])
	if got := meta["score"]; got.Type != TypeFloat || got.Float != 9 {
		t.Fatalf("nested int was not promoted into planned float field: got %v", got)
	}
	if got := meta["tag"]; got.Type != TypeNull {
		t.Fatalf("missing nullable field was not filled with null: got %v", got)
	}

	err := tbl.AddRowTyped([]Value{
		RecordVal([]RecordField{
			{Name: "id", Value: IntVal(2)},
			{Name: "meta", Value: RecordVal([]RecordField{{Name: "score", Value: StrVal("bad")}})},
		}),
	})
	requireSchemaError(t, err, "payload.meta.score", "float", "string")
	if tbl.NumRows != 1 {
		t.Fatalf("failed nested strict append changed row count: got %d", tbl.NumRows)
	}
}

func TestFixedSchemaContractAddRowTypedRejectsDuplicateRecordFields(t *testing.T) {
	schema := recordOf(
		field("x", td(TypeInt)),
		field("meta", recordOf(field("score", td(TypeFloat)))),
		field("items", listOf(recordOf(field("sku", td(TypeString))))),
	)
	cases := []struct {
		name  string
		value Value
		path  string
	}{
		{
			name: "top_level_record",
			value: RecordVal([]RecordField{
				{Name: "x", Value: IntVal(1)},
				{Name: "x", Value: IntVal(2)},
			}),
			path: "payload.x",
		},
		{
			name: "nested_record",
			value: RecordVal([]RecordField{
				{Name: "x", Value: IntVal(1)},
				{Name: "meta", Value: RecordVal([]RecordField{
					{Name: "score", Value: FloatVal(1.5)},
					{Name: "score", Value: FloatVal(2.5)},
				})},
			}),
			path: "payload.meta.score",
		},
		{
			name: "list_record_element",
			value: RecordVal([]RecordField{
				{Name: "x", Value: IntVal(1)},
				{Name: "items", Value: ListVal([]Value{
					RecordVal([]RecordField{
						{Name: "sku", Value: StrVal("a")},
						{Name: "sku", Value: StrVal("b")},
					}),
				})},
			}),
			path: "payload.items[].sku",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := NewTableWithSchemas([]string{"payload"}, []*TypeDescriptor{schema})
			if err := tbl.AddRowTyped([]Value{
				RecordVal([]RecordField{
					{Name: "x", Value: IntVal(0)},
					{Name: "meta", Value: RecordVal([]RecordField{{Name: "score", Value: FloatVal(0)}})},
					{Name: "items", Value: ListVal([]Value{RecordVal([]RecordField{{Name: "sku", Value: StrVal("seed")}})})},
				}),
			}); err != nil {
				t.Fatalf("seed AddRowTyped returned error: %v", err)
			}

			err := tbl.AddRowTyped([]Value{tc.value})
			if err == nil {
				t.Fatal("expected duplicate record field error")
			}
			if got, want := err.Error(), tc.path+" duplicate record field"; got != want {
				t.Fatalf("error: got %q, want %q", got, want)
			}
			if tbl.NumRows != 1 || tbl.Col(0).Len() != 1 {
				t.Fatalf("failed strict append partially modified table: rows=%d len=%d", tbl.NumRows, tbl.Col(0).Len())
			}
		})
	}
}

func TestFixedSchemaContractPermissiveAppendNormalizesWidenedNestedRecordValues(t *testing.T) {
	tbl := NewTable([]string{"r"})
	tbl.AddRow([]Value{RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}})})
	tbl.AddRow([]Value{RecordVal([]RecordField{{Name: "x", Value: StrVal("a")}})})
	tbl.AddRow([]Value{RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}})})

	requireSchemaString(t, tbl.Col(tbl.ColIndex("r")).Schema(), "record<x:string>")

	for i, want := range []string{"1", "a", "2"} {
		r := tbl.Get(i, "r")
		fields := recordValuesForFixedSchemaContract(r)
		got := fields["x"]
		if got.Type != TypeString || got.Str != want {
			t.Fatalf("row %d r.x: got %v, want string %q", i, got, want)
		}
	}
}

func TestFixedSchemaContractPermissiveAppendDuplicateRecordFieldsStringifies(t *testing.T) {
	tbl := NewTable([]string{"r"})
	tbl.AddRow([]Value{
		RecordVal([]RecordField{
			{Name: "x", Value: IntVal(1)},
			{Name: "x", Value: IntVal(2)},
		}),
	})

	if got := tbl.Col(tbl.ColIndex("r")).ColType(); got != TypeString {
		t.Fatalf("column storage type: got %v, want string", got)
	}
	requireSchemaString(t, tbl.Col(tbl.ColIndex("r")).Schema(), "string?")
	got := tbl.Get(0, "r")
	if got.Type != TypeString || got.Str != "{x:1, x:2}" {
		t.Fatalf("duplicate record field value: got %v %q, want stringified record", got.Type, got.Str)
	}
}

func TestFixedSchemaContractPermissiveAppendDuplicateRecordFieldsConvertsExistingRecords(t *testing.T) {
	tbl := NewTable([]string{"r"})
	tbl.AddRow([]Value{RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}})})
	tbl.AddRow([]Value{
		RecordVal([]RecordField{
			{Name: "x", Value: IntVal(2)},
			{Name: "x", Value: IntVal(3)},
		}),
	})

	if got := tbl.Col(tbl.ColIndex("r")).ColType(); got != TypeString {
		t.Fatalf("column storage type: got %v, want string", got)
	}
	requireSchemaString(t, tbl.Col(tbl.ColIndex("r")).Schema(), "string?")
	for i, want := range []string{"{x:1}", "{x:2, x:3}"} {
		got := tbl.Get(i, "r")
		if got.Type != TypeString || got.Str != want {
			t.Fatalf("row %d: got %v %q, want %q", i, got.Type, got.Str, want)
		}
	}
}

func TestFixedSchemaContractPermissiveAppendNormalizesWidenedNestedNumericRecordValues(t *testing.T) {
	tbl := NewTable([]string{"r"})
	tbl.AddRow([]Value{RecordVal([]RecordField{
		{Name: "x", Value: IntVal(1)},
		{Name: "y", Value: IntVal(1)},
		{Name: "deep", Value: RecordVal([]RecordField{{Name: "score", Value: IntVal(10)}})},
	})})
	tbl.AddRow([]Value{RecordVal([]RecordField{
		{Name: "x", Value: FloatVal(2.5)},
		{Name: "y", Value: StrVal("b")},
		{Name: "deep", Value: RecordVal([]RecordField{{Name: "score", Value: FloatVal(20.5)}})},
	})})
	tbl.AddRow([]Value{RecordVal([]RecordField{
		{Name: "x", Value: IntVal(3)},
		{Name: "y", Value: IntVal(2)},
		{Name: "deep", Value: RecordVal([]RecordField{{Name: "score", Value: IntVal(30)}})},
	})})

	requireSchemaString(t, tbl.Col(tbl.ColIndex("r")).Schema(), "record<deep:record<score:float>, x:float, y:string>")

	for i, want := range []float64{1, 2.5, 3} {
		fields := recordValuesForFixedSchemaContract(tbl.Get(i, "r"))
		got := fields["x"]
		if got.Type != TypeFloat || got.Float != want {
			t.Fatalf("row %d r.x: got %v, want float %g", i, got, want)
		}
		deep := recordValuesForFixedSchemaContract(fields["deep"])
		wantScore := []float64{10, 20.5, 30}[i]
		if got := deep["score"]; got.Type != TypeFloat || got.Float != wantScore {
			t.Fatalf("row %d r.deep.score: got %v, want float %g", i, got, wantScore)
		}
	}
	for i, want := range []string{"1", "b", "2"} {
		fields := recordValuesForFixedSchemaContract(tbl.Get(i, "r"))
		got := fields["y"]
		if got.Type != TypeString || got.Str != want {
			t.Fatalf("row %d r.y: got %v, want string %q", i, got, want)
		}
	}
}

func TestFixedSchemaContractAddRowTypedPreservesNestedRecordValueOrder(t *testing.T) {
	schema := &TypeDescriptor{
		Kind: TypeList,
		Elem: recordOf(
			field("age", td(TypeInt)),
			field("name", td(TypeString)),
		),
	}
	tbl := NewTableWithSchemas([]string{"bundle"}, []*TypeDescriptor{schema})

	err := tbl.AddRowTyped([]Value{
		ListVal([]Value{
			RecordVal([]RecordField{
				{Name: "name", Value: StrVal("Alice")},
				{Name: "age", Value: IntVal(30)},
			}),
		}),
	})
	if err != nil {
		t.Fatalf("AddRowTyped returned error: %v", err)
	}

	if got, want := tbl.Get(0, "bundle").AsString(), "[{name:Alice, age:30}]"; got != want {
		t.Fatalf("bundle: got %q, want %q", got, want)
	}
}

func TestFixedSchemaContractPermissiveAppendPreservesNestedRecordValueOrderWhenWidening(t *testing.T) {
	tbl := NewTable([]string{"bundle"})
	tbl.AddRow([]Value{
		ListVal([]Value{
			RecordVal([]RecordField{
				{Name: "name", Value: StrVal("Alice")},
				{Name: "age", Value: IntVal(30)},
			}),
		}),
	})
	tbl.AddRow([]Value{
		ListVal([]Value{
			RecordVal([]RecordField{
				{Name: "name", Value: StrVal("Bob")},
				{Name: "age", Value: StrVal("25")},
			}),
		}),
	})

	if got, want := tbl.Get(0, "bundle").AsString(), "[{name:Alice, age:30}]"; got != want {
		t.Fatalf("row 0 bundle: got %q, want %q", got, want)
	}
	if got, want := tbl.Get(1, "bundle").AsString(), "[{name:Bob, age:25}]"; got != want {
		t.Fatalf("row 1 bundle: got %q, want %q", got, want)
	}
}

func TestFixedSchemaContractPermissiveAppendNormalizesWidenedNestedListValues(t *testing.T) {
	tbl := NewTable([]string{"xs"})
	tbl.AddRow([]Value{ListVal([]Value{RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}})})})
	tbl.AddRow([]Value{ListVal([]Value{RecordVal([]RecordField{{Name: "x", Value: StrVal("a")}})})})
	tbl.AddRow([]Value{ListVal([]Value{RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}})})})

	requireSchemaString(t, tbl.Col(tbl.ColIndex("xs")).Schema(), "list<record<x:string>>")

	for i, want := range []string{"1", "a", "2"} {
		xs := tbl.Get(i, "xs")
		if xs.Type != TypeList || len(xs.List) != 1 {
			t.Fatalf("row %d xs: got %v, want one-item list", i, xs)
		}
		fields := recordValuesForFixedSchemaContract(xs.List[0])
		got := fields["x"]
		if got.Type != TypeString || got.Str != want {
			t.Fatalf("row %d xs[0].x: got %v, want string %q", i, got, want)
		}
	}
}

func TestFixedSchemaContractPermissiveAppendNormalizesWidenedNestedNumericScalarListValues(t *testing.T) {
	tbl := NewTable([]string{"xs"})
	tbl.AddRow([]Value{ListVal([]Value{IntVal(1), IntVal(2)})})
	tbl.AddRow([]Value{ListVal([]Value{FloatVal(2.5), FloatVal(3.5)})})
	tbl.AddRow([]Value{ListVal([]Value{IntVal(4)})})

	requireSchemaString(t, tbl.Col(tbl.ColIndex("xs")).Schema(), "list<float>")

	wantRows := [][]float64{
		{1, 2},
		{2.5, 3.5},
		{4},
	}
	for row, want := range wantRows {
		xs := tbl.Get(row, "xs")
		if xs.Type != TypeList || len(xs.List) != len(want) {
			t.Fatalf("row %d xs: got %v, want %d float items", row, xs, len(want))
		}
		for i, wantItem := range want {
			got := xs.List[i]
			if got.Type != TypeFloat || got.Float != wantItem {
				t.Fatalf("row %d xs[%d]: got %v, want float %g", row, i, got, wantItem)
			}
		}
	}
}

func TestFixedSchemaContractPermissiveAppendNormalizesWidenedNestedNumericListValues(t *testing.T) {
	tbl := NewTable([]string{"xs"})
	tbl.AddRow([]Value{ListVal([]Value{
		RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}, {Name: "y", Value: IntVal(1)}}),
	})})
	tbl.AddRow([]Value{ListVal([]Value{
		RecordVal([]RecordField{{Name: "x", Value: FloatVal(2.5)}, {Name: "y", Value: StrVal("b")}}),
	})})
	tbl.AddRow([]Value{ListVal([]Value{
		RecordVal([]RecordField{{Name: "x", Value: IntVal(3)}, {Name: "y", Value: IntVal(2)}}),
	})})

	requireSchemaString(t, tbl.Col(tbl.ColIndex("xs")).Schema(), "list<record<x:float, y:string>>")

	for i, want := range []float64{1, 2.5, 3} {
		xs := tbl.Get(i, "xs")
		if xs.Type != TypeList || len(xs.List) != 1 {
			t.Fatalf("row %d xs: got %v, want one-item list", i, xs)
		}
		fields := recordValuesForFixedSchemaContract(xs.List[0])
		got := fields["x"]
		if got.Type != TypeFloat || got.Float != want {
			t.Fatalf("row %d xs[0].x: got %v, want float %g", i, got, want)
		}
	}
	for i, want := range []string{"1", "b", "2"} {
		fields := recordValuesForFixedSchemaContract(tbl.Get(i, "xs").List[0])
		got := fields["y"]
		if got.Type != TypeString || got.Str != want {
			t.Fatalf("row %d xs[0].y: got %v, want string %q", i, got, want)
		}
	}
}

func recordValuesForFixedSchemaContract(v Value) map[string]Value {
	out := make(map[string]Value, len(v.Fields))
	if v.Type != TypeRecord {
		return out
	}
	for _, field := range v.Fields {
		out[field.Name] = field.Value
	}
	return out
}
