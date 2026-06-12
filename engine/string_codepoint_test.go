package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

// String builtins use 0-based Unicode code point indices (not UTF-8 byte offsets).
// substr() negative start counts from the end (Python-style); length must be non-negative.
// substr() must stay consistent with str_len().

func unicodeStringsTable() *table.Table {
	tbl := table.NewTable([]string{"text"})
	tbl.AddRow([]table.Value{table.StrVal("日本語")})
	return tbl
}

func TestStrLenUnicodeCodePoints(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  int64
	}{
		{"ascii", `transform n = str_len("hello") | select n`, 5},
		{"latin_accent", `transform n = str_len("café") | select n`, 4},
		{"cjk", `transform n = str_len("日本") | select n`, 2},
		{"emoji", `transform n = str_len("👋") | select n`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, typedValuesTable(), tc.query)
			if result.GetAt(0, 0).Int != tc.want {
				t.Errorf("want %d code points, got %d", tc.want, result.GetAt(0, 0).Int)
			}
		})
	}
}

func TestSubstrCodePointIndexing(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"ascii_mid", `transform s = substr("hello", 1, 3) | select s`, "ell"},
		{"ascii_from_start", `transform s = substr("hello", 0, 2) | select s`, "he"},
		{"latin_accent_slice", `transform s = substr("café", 0, 3) | select s`, "caf"},
		{"latin_accent_char", `transform s = substr("café", 3, 1) | select s`, "é"},
		{"cjk_first_rune", `transform s = substr("日本語", 0, 1) | select s`, "日"},
		{"cjk_second_rune", `transform s = substr("日本語", 1, 1) | select s`, "本"},
		{"emoji_whole", `transform s = substr("👋🌍", 0, 1) | select s`, "👋"},
		{"emoji_second", `transform s = substr("👋🌍", 1, 1) | select s`, "🌍"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, typedValuesTable(), tc.query)
			got := result.GetAt(0, 0).Str
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSubstrFullStringViaStrLen(t *testing.T) {
	// str_len and substr must share code-point units; byte-based substr breaks this.
	cases := []string{`"hello"`, `"café"`, `"日本語"`, `"👋🌍"`}
	for _, lit := range cases {
		t.Run(lit, func(t *testing.T) {
			query := "transform s = substr(" + lit + ", 0, str_len(" + lit + ")) | select s"
			result := runQuery(t, typedValuesTable(), query)
			got := result.GetAt(0, 0).Str
			ref := runQuery(t, typedValuesTable(), "transform s = "+lit+" | select s")
			want := ref.GetAt(0, 0).Str
			if got != want {
				t.Errorf("substr(0, str_len(s)) = %q, want %q", got, want)
			}
		})
	}
}

func TestSubstrFromColumnCodePoints(t *testing.T) {
	result := runQuery(t, unicodeStringsTable(), "transform s = substr(text, 1, 2) | select s")
	if result.GetAt(0, 0).Str != "本語" {
		t.Errorf("want %q, got %q", "本語", result.GetAt(0, 0).Str)
	}
}

func TestSubstrStartAtCodePointLength(t *testing.T) {
	result := runQuery(t, typedValuesTable(), `transform s = substr("café", 0, 1) | select s`)
	if result.GetAt(0, 0).Str != "c" {
		t.Errorf("want %q, got %q", "c", result.GetAt(0, 0).Str)
	}
}

func TestSubstrEmptyWhenStartBeyondCodePoints(t *testing.T) {
	result := runQuery(t, typedValuesTable(), `transform s = substr("hi", 5, 1) | select s`)
	if result.GetAt(0, 0).Str != "" {
		t.Errorf("want empty string, got %q", result.GetAt(0, 0).Str)
	}
}

func TestSubstrZeroLength(t *testing.T) {
	result := runQuery(t, typedValuesTable(), `transform s = substr("hello", 1, 0) | select s`)
	if result.GetAt(0, 0).Str != "" {
		t.Errorf("want empty string for zero length, got %q", result.GetAt(0, 0).Str)
	}
}

func TestStrLenInvalidUTF8(t *testing.T) {
	tbl := table.NewTable([]string{"s"})
	tbl.AddRow([]table.Value{table.StrVal(string([]byte{0x61, 0x62, 0xff, 0x63}))}) // "ab" + invalid + "c"
	result := runQuery(t, tbl, "transform n = str_len(s) | select n")
	if result.GetAt(0, 0).Int != 4 {
		t.Errorf("invalid UTF-8: want 4 code points, got %d", result.GetAt(0, 0).Int)
	}
}

func TestSubstrInvalidUTF8ConsistentWithStrLen(t *testing.T) {
	tbl := table.NewTable([]string{"s"})
	s := string([]byte{0x61, 0x62, 0xff, 0x63})
	tbl.AddRow([]table.Value{table.StrVal(s)})

	result := runQuery(t, tbl, "transform part = substr(s, 0, 2), round_len = str_len(substr(s, 0, str_len(s))) | select part, round_len")
	part := result.GetAt(0, 0).Str
	roundLen := result.GetAt(0, 1).Int
	if part != "ab" {
		t.Errorf("substr(0, 2): want %q, got %q", "ab", part)
	}
	if roundLen != 4 {
		t.Errorf("str_len(substr(s, 0, str_len(s))): want 4 code points, got %d", roundLen)
	}
}

func TestSubstrNegativeStartFromEnd(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"ascii_last_two", `transform s = substr("hello", -2, 2) | select s`, "lo"},
		{"ascii_last_one", `transform s = substr("hello", -1, 1) | select s`, "o"},
		{"ascii_clamp_start", `transform s = substr("hello", -10, 2) | select s`, "he"},
		{"cjk_last", `transform s = substr("日本語", -1, 1) | select s`, "語"},
		{"emoji_last", `transform s = substr("👋🌍", -1, 1) | select s`, "🌍"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, typedValuesTable(), tc.query)
			got := result.GetAt(0, 0).Str
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSubstrNegativeLengthError(t *testing.T) {
	expectQueryErrContains(t, typedValuesTable(), `transform s = substr("hello", 0, -1) | select s`, "length must not be negative")
}

func TestCodePointIndexOutOfRange(t *testing.T) {
	maxInt := int64(int(^uint(0) >> 1))

	t.Run("max_int_accepted", func(t *testing.T) {
		for _, field := range []string{"start", "length"} {
			t.Run(field, func(t *testing.T) {
				idx, err := codePointIndex(maxInt, field)
				if err != nil {
					t.Fatalf("max int should be accepted for %s: %v", field, err)
				}
				if idx != int(maxInt) {
					t.Fatalf("want %d, got %d", maxInt, idx)
				}
			})
		}
	})

	if strconv.IntSize != 32 {
		t.Skipf("int index overflow guard only reachable on 32-bit int (got %d-bit)", strconv.IntSize)
	}

	maxInt32 := int64(int(^uint(0) >> 1))
	over := maxInt32 + 1
	cases := []struct {
		name  string
		value int64
		field string
	}{
		{"start", over, "start"},
		{"length", over, "length"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := codePointIndex(tc.value, tc.field)
			if err == nil {
				t.Fatal("expected out of range error")
			}
			want := "substr: " + tc.field + " out of range"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error containing %q, got: %v", want, err)
			}
		})
	}
}

func TestSubstrIndexOutOfRangeErrors(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skipf("substr index overflow errors only occur on 32-bit int (got %d-bit)", strconv.IntSize)
	}

	maxInt32 := int64(int(^uint(0) >> 1))
	over := maxInt32 + 1

	t.Run("start", func(t *testing.T) {
		tbl := table.NewTable([]string{"s", "start", "length"})
		tbl.AddRow([]table.Value{table.StrVal("hello"), table.IntVal(over), table.IntVal(1)})
		expectQueryErrContains(t, tbl, "transform out = substr(s, start, length) | select out", "substr: start out of range")
	})

	t.Run("length", func(t *testing.T) {
		tbl := table.NewTable([]string{"s", "start", "length"})
		tbl.AddRow([]table.Value{table.StrVal("hello"), table.IntVal(0), table.IntVal(over)})
		expectQueryErrContains(t, tbl, "transform out = substr(s, start, length) | select out", "substr: length out of range")
	})
}

func TestSubstrNonIntIndexErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"float_start", `transform s = substr("hello", 1.0, 1) | select s`, "substr: start must be an int, got float"},
		{"float_length", `transform s = substr("hello", 0, 1.0) | select s`, "substr: length must be an int, got float"},
		{"string_start", `transform s = substr("hello", name, 1) | select s`, "substr: start must be an int, got string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.wantErr)
		})
	}
}

func TestSubstrWrongTypeFirstArgErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"list", "transform s = substr(xs, 0, 1)", "substr() requires a string, got list"},
		{"record", "transform s = substr(rec, 0, 1)", "substr() requires a string, got record"},
		{"bool", "transform s = substr(flag, 0, 1)", "substr() requires a string, got bool"},
		{"float", "transform s = substr(price, 0, 1)", "substr() requires a string, got float"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, typedValuesTable(), tc.query, tc.wantErr)
		})
	}
}

func TestSubstrFloatColumnErrors(t *testing.T) {
	t.Run("float_start_column", func(t *testing.T) {
		tbl := table.NewTable([]string{"s", "start", "length"})
		tbl.AddRow([]table.Value{table.StrVal("hello"), table.FloatVal(1), table.IntVal(1)})
		expectQueryErrContains(t, tbl, "transform out = substr(s, start, length) | select out", "substr: start must be an int, got float")
	})
	t.Run("float_length_column", func(t *testing.T) {
		tbl := table.NewTable([]string{"s", "start", "length"})
		tbl.AddRow([]table.Value{table.StrVal("hello"), table.IntVal(0), table.FloatVal(1)})
		expectQueryErrContains(t, tbl, "transform out = substr(s, start, length) | select out", "substr: length must be an int, got float")
	})
}

func TestSubstrNullPropagation(t *testing.T) {
	tbl := table.NewTable([]string{"s", "n"})
	tbl.AddRow([]table.Value{table.StrVal("hello"), table.Null()})
	cases := []struct {
		name  string
		query string
	}{
		{"null_string", "transform s = substr(nilcol, 0, 1) | select s"},
		{"null_start", "transform s = substr(s, n, 1) | select s"},
		{"null_length", "transform s = substr(s, 0, n) | select s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := typedValuesTable()
			if tc.name == "null_start" || tc.name == "null_length" {
				input = tbl
			}
			result := runQuery(t, input, tc.query)
			if !result.GetAt(0, 0).IsNull() {
				t.Errorf("%s: want null, got %v", tc.name, result.GetAt(0, 0).AsString())
			}
		})
	}
}
