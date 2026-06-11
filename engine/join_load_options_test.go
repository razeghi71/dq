package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func joinWithLoadFunc(t *testing.T) func(string, ast.LoadOptions) (*table.Table, error) {
	t.Helper()
	return func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	}
}

func loadUsersCSV(t *testing.T, dir string) (*table.Table, string) {
	t.Helper()
	usersPath := filepath.Join(dir, "users.csv")
	if err := os.WriteFile(usersPath, []byte("name\nAlice\nBob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := loader.Load(usersPath, loader.Options{})
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	return tbl, usersPath
}

func TestIntegrationJoinWithLoadOptions(t *testing.T) {
	t.Run("literal_format_override", func(t *testing.T) {
		dir := t.TempDir()
		left, usersPath := loadUsersCSV(t, dir)
		ordersPath := filepath.Join(dir, "orders.dat")
		if err := os.WriteFile(ordersPath, []byte("user_name,status\nAlice,shipped\nBob,pending\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv on name == user_name | sort name`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Load.Format != "csv" {
			t.Fatalf("join load format: got %q", j.Load.Format)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if result.NumRows != 2 {
			t.Fatalf("expected 2 rows, got %d: %s", result.NumRows, result.String())
		}
		if result.Get(0, "status").Str != "shipped" || result.Get(1, "status").Str != "pending" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("glob_format_override", func(t *testing.T) {
		dir := t.TempDir()
		left, usersPath := loadUsersCSV(t, dir)
		ordersGlob := filepath.Join(dir, "orders-*.dat")
		if err := os.WriteFile(filepath.Join(dir, "orders-001.dat"), []byte("user_name,status\nAlice,shipped\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "orders-002.dat"), []byte("user_name,status\nBob,pending\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersGlob + ` with format=csv on name == user_name | sort name`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if result.NumRows != 2 {
			t.Fatalf("expected 2 rows, got %d", result.NumRows)
		}
		if result.Get(0, "name").Str != "Alice" || result.Get(0, "status").Str != "shipped" {
			t.Errorf("row 0: got %s", result.String())
		}
		if result.Get(1, "name").Str != "Bob" || result.Get(1, "status").Str != "pending" {
			t.Errorf("row 1: got %s", result.String())
		}
	})

	t.Run("delim_semicolon", func(t *testing.T) {
		dir := t.TempDir()
		usersPath := filepath.Join(dir, "users.csv")
		ordersPath := filepath.Join(dir, "orders.dat")
		if err := os.WriteFile(usersPath, []byte("user_id\n1\n2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ordersPath, []byte("user_id;amount\n1;100\n2;200\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		left, err := loader.Load(usersPath, loader.Options{})
		if err != nil {
			t.Fatalf("load users: %v", err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv, delim=";" on user_id | sort user_id`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Load.Delim != ";" {
			t.Fatalf("join delim: got %q", j.Load.Delim)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if result.NumRows != 2 {
			t.Fatalf("expected 2 rows, got %d: %s", result.NumRows, result.String())
		}
		if result.Get(0, "amount").Int != 100 || result.Get(1, "amount").Int != 200 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("left_join_delim", func(t *testing.T) {
		dir := t.TempDir()
		usersPath := filepath.Join(dir, "users.csv")
		ordersPath := filepath.Join(dir, "orders.dat")
		if err := os.WriteFile(usersPath, []byte("user_id\n1\n3\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ordersPath, []byte("user_id;amount\n1;100\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		left, err := loader.Load(usersPath, loader.Options{})
		if err != nil {
			t.Fatalf("load users: %v", err)
		}

		q, err := parser.Parse(usersPath + ` | join left ` + ordersPath + ` with format=csv, delim=";" on user_id | sort user_id`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if result.NumRows != 2 {
			t.Fatalf("expected 2 rows, got %d: %s", result.NumRows, result.String())
		}
		if result.Get(0, "amount").Int != 100 {
			t.Fatalf("matched row: got %s", result.String())
		}
		if result.Get(1, "amount").Type != table.TypeNull {
			t.Fatalf("unmatched left row should have null amount, got %s", result.String())
		}
	})

	t.Run("glob_delim", func(t *testing.T) {
		dir := t.TempDir()
		usersPath := filepath.Join(dir, "users.csv")
		ordersGlob := filepath.Join(dir, "orders-*.dat")
		if err := os.WriteFile(usersPath, []byte("user_id\n1\n2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "orders-001.dat"), []byte("user_id;amount\n1;100\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "orders-002.dat"), []byte("user_id;amount\n2;200\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		left, err := loader.Load(usersPath, loader.Options{})
		if err != nil {
			t.Fatalf("load users: %v", err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersGlob + ` with format=csv, delim=";" on user_id | sort user_id`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if result.NumRows != 2 || result.Get(0, "amount").Int != 100 || result.Get(1, "amount").Int != 200 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("header_false", func(t *testing.T) {
		dir := t.TempDir()
		left, usersPath := loadUsersCSV(t, dir)
		ordersPath := filepath.Join(dir, "orders.dat")
		if err := os.WriteFile(ordersPath, []byte("Alice,shipped\nBob,pending\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv, header=false on name == col1 | sort name`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Load.Header == nil || *j.Load.Header {
			t.Fatalf("join header=false: got %v", j.Load.Header)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if result.NumRows != 2 {
			t.Fatalf("expected 2 rows, got %d: %s", result.NumRows, result.String())
		}
		if result.Get(0, "col2").Str != "shipped" || result.Get(1, "col2").Str != "pending" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("format_case_insensitive", func(t *testing.T) {
		dir := t.TempDir()
		left, usersPath := loadUsersCSV(t, dir)
		ordersPath := filepath.Join(dir, "orders.dat")
		if err := os.WriteFile(ordersPath, []byte("user_name,status\nAlice,shipped\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=CSV on name == user_name`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if result.NumRows != 1 || result.Get(0, "status").Str != "shipped" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("load_callback_receives_opts", func(t *testing.T) {
		dir := t.TempDir()
		left, usersPath := loadUsersCSV(t, dir)
		ordersPath := filepath.Join(dir, "orders.dat")
		if err := os.WriteFile(ordersPath, []byte("name;status\nAlice;shipped\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv, delim=";" on name == name`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		var gotPath string
		var gotOpts ast.LoadOptions
		loadFunc := func(filename string, opts ast.LoadOptions) (*table.Table, error) {
			gotPath = filename
			gotOpts = opts
			return loader.Load(filename, loader.FromAST(opts))
		}

		result, err := Execute(q, left, loadFunc)
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if gotPath != ordersPath {
			t.Fatalf("load path: got %q want %q", gotPath, ordersPath)
		}
		if gotOpts.Format != "csv" || gotOpts.Delim != ";" {
			t.Fatalf("load opts: got format=%q delim=%q", gotOpts.Format, gotOpts.Delim)
		}
		if result.NumRows != 1 || result.Get(0, "status").Str != "shipped" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("missing_delim_errors_on_join_key", func(t *testing.T) {
		dir := t.TempDir()
		usersPath := filepath.Join(dir, "users.csv")
		ordersPath := filepath.Join(dir, "orders.dat")
		if err := os.WriteFile(usersPath, []byte("user_id\n1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ordersPath, []byte("user_id;amount\n1;100\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		left, err := loader.Load(usersPath, loader.Options{})
		if err != nil {
			t.Fatalf("load users: %v", err)
		}

		q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv on user_id`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		result, err := Execute(q, left, joinWithLoadFunc(t))
		if err == nil {
			t.Fatal("expected join error when semicolon file is loaded with comma delimiter")
		}
		if result != nil {
			t.Fatalf("expected nil result on error, got %s", result.String())
		}
	})
}
