package lexer

import (
	"testing"
)

func TestLexWithKeyword(t *testing.T) {
	tokens, err := Lex("data with format=csv")
	if err != nil {
		t.Fatal(err)
	}
	want := []TokenType{TokenIdent, TokenWith, TokenIdent, TokenEquals, TokenIdent, TokenEOF}
	if len(tokens) != len(want) {
		t.Fatalf("expected %d tokens, got %d", len(want), len(tokens))
	}
	for i, tt := range want {
		if tokens[i].Type != tt {
			t.Errorf("token %d: expected %s, got %s (%q)", i, tt, tokens[i].Type, tokens[i].Val)
		}
	}
}

func TestLexJoinWithLoadOptions(t *testing.T) {
	tokens, err := Lex(`users.csv | join orders.dat with format=csv, delim=";" on id`)
	if err != nil {
		t.Fatal(err)
	}
	want := []TokenType{
		TokenIdent, TokenDot, TokenIdent, TokenPipe, TokenIdent,
		TokenIdent, TokenDot, TokenIdent, TokenWith,
		TokenIdent, TokenEquals, TokenIdent, TokenComma,
		TokenIdent, TokenEquals, TokenString, TokenIdent, TokenIdent, TokenEOF,
	}
	if len(tokens) != len(want) {
		t.Fatalf("expected %d tokens, got %d", len(want), len(tokens))
	}
	for i, tt := range want {
		if tokens[i].Type != tt {
			t.Errorf("token %d: expected %s, got %s (%q)", i, tt, tokens[i].Type, tokens[i].Val)
		}
	}
}

func TestLexBasic(t *testing.T) {
	tokens, err := Lex(`users.csv | head 10`)
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{TokenIdent, TokenDot, TokenIdent, TokenPipe, TokenIdent, TokenInt, TokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: expected %s, got %s (%q)", i, tt, tokens[i].Type, tokens[i].Val)
		}
	}
}

func TestLexFilter(t *testing.T) {
	tokens, err := Lex(`filter { age > 20 and city == "NY" }`)
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{
		TokenIdent, TokenLBrace, TokenIdent, TokenGt, TokenInt,
		TokenAnd, TokenIdent, TokenEq, TokenString, TokenRBrace, TokenEOF,
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: expected %s, got %s (%q)", i, tt, tokens[i].Type, tokens[i].Val)
		}
	}
	// Check string value
	if tokens[8].Val != "NY" {
		t.Errorf("string token value: expected 'NY', got %q", tokens[8].Val)
	}
}

func TestLexBacktick(t *testing.T) {
	tokens, err := Lex("`first name`")
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Type != TokenBacktickIdent {
		t.Errorf("expected backtick ident, got %s", tokens[0].Type)
	}
	if tokens[0].Val != "first name" {
		t.Errorf("expected 'first name', got %q", tokens[0].Val)
	}
}

func TestLexFloats(t *testing.T) {
	tokens, err := Lex("3.14")
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Type != TokenFloat {
		t.Errorf("expected FLOAT, got %s", tokens[0].Type)
	}
	if tokens[0].Val != "3.14" {
		t.Errorf("expected '3.14', got %q", tokens[0].Val)
	}
}

func TestLexNegativeNumber(t *testing.T) {
	tokens, err := Lex("age > -5")
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{TokenIdent, TokenGt, TokenInt, TokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	if tokens[2].Val != "-5" {
		t.Errorf("expected '-5', got %q", tokens[2].Val)
	}
}

func TestLexOperators(t *testing.T) {
	tokens, err := Lex("== != <= >= < > + - * /")
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{
		TokenEq, TokenNeq, TokenLte, TokenGte, TokenLt, TokenGt,
		TokenPlus, TokenMinus, TokenStar, TokenSlash, TokenEOF,
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: expected %s, got %s", i, tt, tokens[i].Type)
		}
	}
}

func TestLexIsNull(t *testing.T) {
	tokens, err := Lex("age is not null")
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{TokenIdent, TokenIs, TokenNot, TokenNull, TokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
}

func TestLexStringEscape(t *testing.T) {
	tokens, err := Lex(`"hello \"world\""`)
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Val != `hello "world"` {
		t.Errorf("expected 'hello \"world\"', got %q", tokens[0].Val)
	}
}

func TestScanSourceStdin(t *testing.T) {
	l := NewLexer("- | head 10")
	tok, err := l.ScanSource()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Type != TokenStdin || tok.Val != "-" {
		t.Errorf("expected STDIN '-', got %s %q", tok.Type, tok.Val)
	}
	tok, err = l.Next()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Type != TokenPipe {
		t.Errorf("expected pipe after stdin, got %s", tok.Type)
	}
}

func TestScanSourceStdinAdjacentPipe(t *testing.T) {
	l := NewLexer("-| head 10")
	tok, err := l.ScanSource()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Type != TokenStdin || tok.Val != "-" {
		t.Fatalf("expected STDIN '-', got %s %q", tok.Type, tok.Val)
	}
	tok, err = l.Next()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Type != TokenPipe || tok.Pos != 1 {
		t.Fatalf("expected pipe at position 1, got %s @%d", tok.Type, tok.Pos)
	}
}

func TestScanSourceHyphenatedFilename(t *testing.T) {
	l := NewLexer("my-file.csv | head")
	tok, err := l.ScanSource()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Type != TokenIdent || tok.Val != "my-file.csv" {
		t.Errorf("expected IDENT my-file.csv, got %s %q", tok.Type, tok.Val)
	}
}

func TestLexComment(t *testing.T) {
	tokens, err := Lex("age // this is a comment\n+ 5")
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{TokenIdent, TokenPlus, TokenInt, TokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
}

func TestLexCommentAtEOF(t *testing.T) {
	tokens, err := Lex("age // trailing comment")
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{TokenIdent, TokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %#v", len(expected), tokens)
	}
	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, tt, tokens[i].Type, tokens[i].Val)
		}
	}
	if tokens[1].Pos != len([]rune("age // trailing comment")) {
		t.Fatalf("EOF position: got %d", tokens[1].Pos)
	}
}

func TestLexUnderscoreIdentifier(t *testing.T) {
	tokens, err := Lex("_name")
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected identifier and EOF, got %#v", tokens)
	}
	if tokens[0].Type != TokenIdent || tokens[0].Val != "_name" {
		t.Fatalf("expected _name identifier, got %s %q", tokens[0].Type, tokens[0].Val)
	}
}

func TestLexSingleCharacterTokensAtEOF(t *testing.T) {
	cases := []struct {
		input string
		typ   TokenType
		val   string
	}{
		{"|", TokenPipe, "|"},
		{"{", TokenLBrace, "{"},
		{"}", TokenRBrace, "}"},
		{"(", TokenLParen, "("},
		{")", TokenRParen, ")"},
		{",", TokenComma, ","},
		{".", TokenDot, "."},
		{"+", TokenPlus, "+"},
		{"-", TokenMinus, "-"},
		{"*", TokenStar, "*"},
		{"/", TokenSlash, "/"},
		{"=", TokenEquals, "="},
		{"<", TokenLt, "<"},
		{">", TokenGt, ">"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			l := NewLexer(tc.input)
			tok, err := l.Next()
			if err != nil {
				t.Fatalf("first token: %v", err)
			}
			if tok.Type != tc.typ || tok.Val != tc.val || tok.Pos != 0 {
				t.Fatalf("first token: want %s %q @0, got %s %q @%d", tc.typ, tc.val, tok.Type, tok.Val, tok.Pos)
			}
			eof, err := l.Next()
			if err != nil {
				t.Fatalf("EOF token: %v", err)
			}
			if eof.Type != TokenEOF || eof.Pos != len([]rune(tc.input)) {
				t.Fatalf("EOF token: want EOF at %d, got %s @%d", len([]rune(tc.input)), eof.Type, eof.Pos)
			}
		})
	}
}

func TestLexTwoCharacterOperatorBoundaries(t *testing.T) {
	cases := []struct {
		input string
		typ   TokenType
		val   string
	}{
		{"==", TokenEq, "=="},
		{"!=", TokenNeq, "!="},
		{"<=", TokenLte, "<="},
		{">=", TokenGte, ">="},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			tokens, err := Lex(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if len(tokens) != 2 {
				t.Fatalf("expected operator and EOF, got %#v", tokens)
			}
			if tokens[0].Type != tc.typ || tokens[0].Val != tc.val {
				t.Fatalf("token: want %s %q, got %s %q", tc.typ, tc.val, tokens[0].Type, tokens[0].Val)
			}
			if tokens[1].Type != TokenEOF || tokens[1].Pos != len([]rune(tc.input)) {
				t.Fatalf("EOF: got %s @%d", tokens[1].Type, tokens[1].Pos)
			}
		})
	}

	if _, err := Lex("!"); err == nil {
		t.Fatal("expected bare ! to error")
	}
}

func TestLexStringAndNumberBoundaries(t *testing.T) {
	t.Run("empty_string", func(t *testing.T) {
		tokens, err := Lex(`""`)
		if err != nil {
			t.Fatal(err)
		}
		if tokens[0].Type != TokenString || tokens[0].Val != "" {
			t.Fatalf("expected empty string token, got %s %q", tokens[0].Type, tokens[0].Val)
		}
	})

	t.Run("trailing_escape_is_literal_until_unterminated", func(t *testing.T) {
		_, err := Lex(`"unterminated\`)
		if err == nil {
			t.Fatal("expected unterminated string error")
		}
	})

	t.Run("integer_followed_by_dot_is_not_float", func(t *testing.T) {
		tokens, err := Lex("1.")
		if err != nil {
			t.Fatal(err)
		}
		if len(tokens) != 3 || tokens[0].Type != TokenInt || tokens[0].Val != "1" || tokens[1].Type != TokenDot {
			t.Fatalf("expected INT(1), DOT, EOF; got %#v", tokens)
		}
	})

	t.Run("negative_number_at_start", func(t *testing.T) {
		tokens, err := Lex("-5")
		if err != nil {
			t.Fatal(err)
		}
		if len(tokens) != 2 || tokens[0].Type != TokenInt || tokens[0].Val != "-5" {
			t.Fatalf("expected INT(-5), EOF; got %#v", tokens)
		}
	})
}

func TestLexBacktickAndSourceBoundaries(t *testing.T) {
	t.Run("empty_backtick_identifier", func(t *testing.T) {
		tokens, err := Lex("``")
		if err != nil {
			t.Fatal(err)
		}
		if tokens[0].Type != TokenBacktickIdent || tokens[0].Val != "" {
			t.Fatalf("expected empty backtick identifier, got %s %q", tokens[0].Type, tokens[0].Val)
		}
	})

	t.Run("source_at_eof", func(t *testing.T) {
		l := NewLexer("users.csv")
		tok, err := l.ScanSource()
		if err != nil {
			t.Fatal(err)
		}
		if tok.Type != TokenIdent || tok.Val != "users.csv" {
			t.Fatalf("expected source filename, got %s %q", tok.Type, tok.Val)
		}
		eof, err := l.Next()
		if err != nil {
			t.Fatal(err)
		}
		if eof.Type != TokenEOF {
			t.Fatalf("expected EOF after source, got %s", eof.Type)
		}
	})

	t.Run("stdin_at_eof", func(t *testing.T) {
		l := NewLexer("-")
		tok, err := l.ScanSource()
		if err != nil {
			t.Fatal(err)
		}
		if tok.Type != TokenStdin || tok.Val != "-" {
			t.Fatalf("expected stdin token, got %s %q", tok.Type, tok.Val)
		}
	})

	t.Run("all_whitespace_source", func(t *testing.T) {
		l := NewLexer(" \t\n")
		tok, err := l.ScanSource()
		if err != nil {
			t.Fatal(err)
		}
		if tok.Type != TokenEOF || tok.Pos != 3 {
			t.Fatalf("expected EOF after whitespace, got %s @%d", tok.Type, tok.Pos)
		}
	})
}
