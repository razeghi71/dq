package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func TestRightJoinDemandPruningTDDSourceAwareJoinIRDoesNotOwnRightRows(t *testing.T) {
	tablePtr := reflect.TypeOf((*table.Table)(nil))

	logical := reflect.TypeOf(logicalJoin{})
	if field, ok := logical.FieldByName("right"); ok && field.Type == tablePtr {
		t.Fatalf("logicalJoin.right is %s; source-aware logical joins must carry a right source/schema witness, not loaded rows", field.Type)
	}
	if _, ok := logical.FieldByName("filename"); ok {
		t.Fatal("logicalJoin stores filename; source identity must be carried only by PreparedJoinSource")
	}

	planned := reflect.TypeOf(plannedJoin{})
	if field, ok := planned.FieldByName("right"); ok && field.Type == tablePtr {
		t.Fatalf("plannedJoin.right is %s; source-aware planned joins must carry an explicit right source spec/hash-build description, not loaded rows", field.Type)
	}
}

func TestRightJoinDemandPruningTDDSourceAwarePlanningDoesNotAcceptLoadFunc(t *testing.T) {
	loadFunc := reflect.TypeOf((LoadFunc)(nil))

	planSource := reflect.TypeOf(planPhysicalSourceQuery)
	for i := 0; i < planSource.NumIn(); i++ {
		if planSource.In(i) == loadFunc {
			t.Fatalf("planPhysicalSourceQuery argument %d is LoadFunc; source-aware planning should resolve join schemas from prepared source witnesses and produce right SourceLoadSpec values", i)
		}
	}

	planLogical := reflect.TypeOf(planLogicalQueryWithSource)
	for i := 0; i < planLogical.NumIn(); i++ {
		if planLogical.In(i) == loadFunc {
			t.Fatalf("planLogicalQueryWithSource argument %d is LoadFunc; source-aware logical planning should not be able to materialize right rows", i)
		}
	}
}

func TestRightJoinDemandPruningTDDJoinSourceProviderIsPrepareOnly(t *testing.T) {
	provider := reflect.TypeOf((*JoinSourceProvider)(nil)).Elem()
	if _, ok := provider.MethodByName("LoadJoinSource"); ok {
		t.Fatal("JoinSourceProvider exposes LoadJoinSource; execution should load from the prepared source capability, not a provider/source ID pair")
	}
	if _, ok := provider.MethodByName("prepareJoinSource"); ok {
		t.Fatal("JoinSourceProvider exposes private prepareJoinSource; planning should use the same one-method provider algebra as execution")
	}
	if _, ok := provider.MethodByName("joinSourceProvider"); ok {
		t.Fatal("JoinSourceProvider exposes a sealing marker; this package uses a plain one-method provider")
	}

	prepare, ok := provider.MethodByName("PrepareJoinSource")
	if !ok {
		t.Fatal("JoinSourceProvider missing PrepareJoinSource")
	}
	sourceType := reflect.TypeOf(PreparedJoinSource{})
	if prepare.Type.NumOut() != 2 || prepare.Type.Out(0) != sourceType {
		t.Fatalf("PrepareJoinSource output = %v; want PreparedJoinSource, error", prepare.Type)
	}

	if sourceType.Kind() != reflect.Struct {
		t.Fatalf("PreparedJoinSource is %s; want concrete capability value", sourceType.Kind())
	}
	if _, ok := sourceType.MethodByName("LoadOptions"); ok {
		t.Fatal("PreparedJoinSource exposes LoadOptions; load options are consumed during preparation and must not be dead capability state")
	}
	for i := 0; i < sourceType.NumField(); i++ {
		if sourceType.Field(i).Type == reflect.TypeOf(ast.LoadOptions{}) {
			t.Fatal("PreparedJoinSource stores ast.LoadOptions; the prepared loader closure should own format/load decisions")
		}
	}
}

func TestRightJoinDemandPruningTDDPreparedJoinSourceSnapshotsSchema(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	source, err := NewPreparedJoinSource("right.csv", schema, func(spec JoinSourceLoadSpec) (*table.Table, error) {
		return table.NewTableWithSchemas([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}}), nil
	})
	if err != nil {
		t.Fatalf("prepare source: %v", err)
	}

	schema.Columns[0].Name = "mutated"

	gotSchema := source.Schema()
	if len(gotSchema.Columns) != 1 || gotSchema.Columns[0].Name != "id" {
		t.Fatalf("schema was not snapshotted: got %#v", gotSchema)
	}
	gotSchema.Columns[0].Name = "mutated"
	if gotSchemaAgain := source.Schema(); len(gotSchemaAgain.Columns) != 1 || gotSchemaAgain.Columns[0].Name != "id" {
		t.Fatalf("Schema returned mutable internal state: got %#v", gotSchemaAgain)
	}
}

func TestRightJoinDemandPruningTDDPreparedJoinSourceRejectsMissingLoaderAtConstruction(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	if _, err := NewPreparedJoinSource("right.csv", schema, nil); err == nil || !strings.Contains(err.Error(), "not loadable") {
		t.Fatalf("NewPreparedJoinSource nil loader error = %v, want not loadable", err)
	}
}

func TestRightJoinDemandPruningTDDLoadFuncJoinSourceProviderRejectsNilReceiverOrLoader(t *testing.T) {
	var nilProvider *loadFuncJoinSourceProvider
	if _, err := nilProvider.PrepareJoinSource("right.csv", ast.LoadOptions{}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil provider prepare error = %v, want not configured", err)
	}

	emptyProvider := &loadFuncJoinSourceProvider{}
	if _, err := emptyProvider.PrepareJoinSource("right.csv", ast.LoadOptions{}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("empty provider prepare error = %v, want not configured", err)
	}
}

func TestRightJoinDemandPruningTDDExecPlannedJoinWrapsLoadErrorWithFilename(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	source, err := newPreparedJoinSourceFromSnapshot("right.csv", schema, func(JoinSourceLoadSpec) (*table.Table, error) {
		return nil, errors.New("reader failed")
	})
	if err != nil {
		t.Fatalf("prepare source: %v", err)
	}

	_, err = execPlannedJoin(plannedJoin{
		right: plannedJoinRightSource{
			source: source,
			spec:   JoinSourceLoadSpec{Columns: table.SelectedColumns("id")},
			env:    mustSchemaEnvFromSchema(schema),
		},
	}, table.NewTableWithSchemas(nil, nil))
	if err == nil || !strings.Contains(err.Error(), `join: load "right.csv": reader failed`) {
		t.Fatalf("execPlannedJoin load error = %v, want filename wrapper", err)
	}
}

func TestRightJoinDemandPruningTDDValidateJoinRightInputSchemaNilTable(t *testing.T) {
	if err := validateJoinRightInputSchema(schemaEnv{}, table.NewTableWithSchemas(nil, nil)); err != nil {
		t.Fatalf("empty planned schema with empty right table error = %v", err)
	}

	err := validateJoinRightInputSchema(schemaEnv{}, nil)
	if err == nil || !strings.Contains(err.Error(), "right input is nil") {
		t.Fatalf("nil right table error = %v, want nil-table rejection", err)
	}
}

func TestRightJoinDemandPruningTDDLoadFuncPreparedJoinSourceDoesNotReload(t *testing.T) {
	right := table.NewTableWithSchemas(
		[]string{"id", "amount"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeInt}},
	)
	if err := right.AddRowTyped([]table.Value{table.IntVal(1), table.IntVal(10)}); err != nil {
		t.Fatalf("seed right table: %v", err)
	}

	loads := 0
	provider := newLoadFuncJoinSourceProvider(func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		loads++
		return right, nil
	})
	source, err := provider.PrepareJoinSource("right.csv", ast.LoadOptions{})
	if err != nil {
		t.Fatalf("prepare right source: %v", err)
	}
	if loads != 1 {
		t.Fatalf("loads after prepare: got %d, want 1", loads)
	}

	for i := 0; i < 2; i++ {
		loaded, err := source.Load(JoinSourceLoadSpec{Columns: table.SelectedColumns("id")})
		if err != nil {
			t.Fatalf("load prepared right source %d: %v", i, err)
		}
		if !reflect.DeepEqual(loaded.Columns, []string{"id"}) {
			t.Fatalf("loaded columns: got %#v, want [id]", loaded.Columns)
		}
	}
	if loads != 1 {
		t.Fatalf("prepared source reloaded backing table: got %d load calls, want 1", loads)
	}
}
