package lexer

import (
	"testing"
)

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
