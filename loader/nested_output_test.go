package loader

import (
	"testing"

	"github.com/razeghi71/dq/table"
)

// nestedRow holds the expected AsString() value for every column
// of one row in the nested test files.
type nestedRow struct {
	id      string
	name    string
	address string
	tags    string
	orders  string
	profile string
}

// wantNestedRows is the ground truth for nested.{json,jsonl,avro,parquet}.
var wantNestedRows = []nestedRow{
	{
		id:      "1",
		name:    "Alice",
		address: "{city:New York, street:123 Main St, zip:10001}",
		tags:    "[admin, user]",
		orders:  "[{amount:59.99, order_id:101, status:shipped}, {amount:129, order_id:102, status:pending}]",
		profile: "{history:[{date:2024-01-10, events:[login, purchase, logout]}, {date:2024-01-11, events:[]}], stats:{logins:42, score:9.5}}",
	},
	{
		id:      "2",
		name:    "Bob",
		address: "{city:Los Angeles, street:456 Oak Ave, zip:90001}",
		tags:    "[user]",
		orders:  "[{amount:39.99, order_id:201, status:delivered}]",
		profile: "{history:[{date:2024-01-05, events:[login]}], stats:{logins:7, score:6.2}}",
	},
	{
		id:      "3",
		name:    "Charlie",
		address: "{city:Chicago, street:789 Pine Rd, zip:60601}",
		tags:    "[moderator, user, beta]",
		orders:  "[]",
		profile: "{history:[], stats:{logins:0, score:0}}",
	},
}

func assertNestedOutput(t *testing.T, tbl *table.Table) {
	t.Helper()

	if len(tbl.Rows) != len(wantNestedRows) {
		t.Fatalf("row count: want %d, got %d", len(wantNestedRows), len(tbl.Rows))
	}

	col := func(name string) int {
		idx := tbl.ColIndex(name)
		if idx < 0 {
			t.Fatalf("column %q not found; got %v", name, tbl.Columns)
		}
		return idx
	}

	idIdx := col("id")
	nameIdx := col("name")
	addrIdx := col("address")
	tagsIdx := col("tags")
	ordersIdx := col("orders")
	profileIdx := col("profile")

	for i, want := range wantNestedRows {
		row := tbl.Rows[i].Values
		got := nestedRow{
			id:      row[idIdx].AsString(),
			name:    row[nameIdx].AsString(),
			address: row[addrIdx].AsString(),
			tags:    row[tagsIdx].AsString(),
			orders:  row[ordersIdx].AsString(),
			profile: row[profileIdx].AsString(),
		}

		if got.id != want.id {
			t.Errorf("row %d id:      want %q\n                       got  %q", i, want.id, got.id)
		}
		if got.name != want.name {
			t.Errorf("row %d name:    want %q\n                       got  %q", i, want.name, got.name)
		}
		if got.address != want.address {
			t.Errorf("row %d address: want %q\n                       got  %q", i, want.address, got.address)
		}
		if got.tags != want.tags {
			t.Errorf("row %d tags:    want %q\n                       got  %q", i, want.tags, got.tags)
		}
		if got.orders != want.orders {
			t.Errorf("row %d orders:  want %q\n                       got  %q", i, want.orders, got.orders)
		}
		if got.profile != want.profile {
			t.Errorf("row %d profile: want %q\n                       got  %q", i, want.profile, got.profile)
		}
	}
}

func TestNestedOutputJSON(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.json", "")
	if err != nil {
		t.Fatal(err)
	}
	assertNestedOutput(t, tbl)
}

func TestNestedOutputJSONL(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.jsonl", "")
	if err != nil {
		t.Fatal(err)
	}
	assertNestedOutput(t, tbl)
}

func TestNestedOutputAvro(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.avro", "")
	if err != nil {
		t.Fatal(err)
	}
	assertNestedOutput(t, tbl)
}

func TestNestedOutputParquet(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.parquet", "")
	if err != nil {
		t.Fatal(err)
	}
	assertNestedOutput(t, tbl)
}
