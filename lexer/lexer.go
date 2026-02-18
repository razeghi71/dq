package lexer

import (
	"fmt"
	"unicode"
)

// TokenType represents the type of a lexical token.
type TokenType int

const (
	// Structural
	TokenPipe   TokenType = iota // |
	TokenLBrace                  // {
	TokenRBrace                  // }
	TokenLParen                  // (
	TokenRParen                  // )
	TokenComma                   // ,
	TokenEquals                  // = (assignment)
	TokenDot                     // .

	// Operators
	TokenPlus  // +
	TokenMinus // -
	TokenStar  // *
	TokenSlash // /
	TokenEq    // ==
	TokenNeq   // !=
	TokenLt    // <
	TokenGt    // >
	TokenLte   // <=
	TokenGte   // >=

	// Keywords / logical
	TokenAnd   // and
	TokenOr    // or
	TokenNot   // not
	TokenIs    // is
	TokenTrue  // true
	TokenFalse // false
	TokenNull  // null
	TokenAs    // as

	// Literals
	TokenInt    // integer literal
	TokenFloat  // float literal
	TokenString // "string literal"

	// Identifiers
	TokenIdent         // plain identifier (column name, op name)
	TokenBacktickIdent // `identifier with spaces`

	// End
	TokenEOF
)

var tokenNames = map[TokenType]string{
	TokenPipe: "|", TokenLBrace: "{", TokenRBrace: "}", TokenLParen: "(", TokenRParen: ")",
	TokenComma: ",", TokenEquals: "=", TokenDot: ".",
	TokenPlus: "+", TokenMinus: "-", TokenStar: "*", TokenSlash: "/",
	TokenEq: "==", TokenNeq: "!=", TokenLt: "<", TokenGt: ">", TokenLte: "<=", TokenGte: ">=",
	TokenAnd: "and", TokenOr: "or", TokenNot: "not", TokenIs: "is",
	TokenTrue: "true", TokenFalse: "false", TokenNull: "null", TokenAs: "as",
	TokenInt: "INT", TokenFloat: "FLOAT", TokenString: "STRING",
	TokenIdent: "IDENT", TokenBacktickIdent: "BACKTICK_IDENT", TokenEOF: "EOF",
}

func (t TokenType) String() string {
	if s, ok := tokenNames[t]; ok {
		return s
	}
	return fmt.Sprintf("Token(%d)", int(t))
}

// Token represents a single lexical token.
type Token struct {
	Type TokenType
	Val  string
	Pos  int // byte offset in original input
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q)@%d", t.Type, t.Val, t.Pos)
}

var keywords = map[string]TokenType{
	"and":   TokenAnd,
	"or":    TokenOr,
	"not":   TokenNot,
	"is":    TokenIs,
	"true":  TokenTrue,
	"false": TokenFalse,
	"null":  TokenNull,
	"as":    TokenAs,
}

// Lex tokenizes the input string into a slice of Tokens.
func Lex(input string) ([]Token, error) {
	var tokens []Token
	runes := []rune(input)
	i := 0

	for i < len(runes) {
		ch := runes[i]

		// Skip whitespace
		if unicode.IsSpace(ch) {
			i++
			continue
		}

		// Single/double char operators and structural tokens
		pos := i
		switch ch {
		case '|':
			tokens = append(tokens, Token{TokenPipe, "|", pos})
			i++
			continue
		case '{':
			tokens = append(tokens, Token{TokenLBrace, "{", pos})
			i++
			continue
		case '}':
			tokens = append(tokens, Token{TokenRBrace, "}", pos})
			i++
			continue
		case '(':
			tokens = append(tokens, Token{TokenLParen, "(", pos})
			i++
			continue
		case ')':
			tokens = append(tokens, Token{TokenRParen, ")", pos})
			i++
			continue
		case ',':
			tokens = append(tokens, Token{TokenComma, ",", pos})
			i++
			continue
		case '.':
			tokens = append(tokens, Token{TokenDot, ".", pos})
			i++
			continue
		case '+':
			tokens = append(tokens, Token{TokenPlus, "+", pos})
			i++
			continue
		case '-':
			// Could be negative number or minus operator
			// If next char is digit and previous token is an operator/start, treat as number
			if i+1 < len(runes) && unicode.IsDigit(runes[i+1]) && isNegativeContext(tokens) {
				tok, newI, err := lexNumber(runes, i)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, tok)
				i = newI
				continue
			}
			tokens = append(tokens, Token{TokenMinus, "-", pos})
			i++
			continue
		case '*':
			tokens = append(tokens, Token{TokenStar, "*", pos})
			i++
			continue
		case '/':
			// Check for // comment
			if i+1 < len(runes) && runes[i+1] == '/' {
				// Skip to end of line
				for i < len(runes) && runes[i] != '\n' {
					i++
				}
				continue
			}
			tokens = append(tokens, Token{TokenSlash, "/", pos})
			i++
			continue
		case '=':
			if i+1 < len(runes) && runes[i+1] == '=' {
				tokens = append(tokens, Token{TokenEq, "==", pos})
				i += 2
			} else {
				tokens = append(tokens, Token{TokenEquals, "=", pos})
				i++
			}
			continue
		case '!':
			if i+1 < len(runes) && runes[i+1] == '=' {
				tokens = append(tokens, Token{TokenNeq, "!=", pos})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected character '!' at position %d (did you mean '!='?)", pos)
			}
			continue
		case '<':
			if i+1 < len(runes) && runes[i+1] == '=' {
				tokens = append(tokens, Token{TokenLte, "<=", pos})
				i += 2
			} else {
				tokens = append(tokens, Token{TokenLt, "<", pos})
				i++
			}
			continue
		case '>':
			if i+1 < len(runes) && runes[i+1] == '=' {
				tokens = append(tokens, Token{TokenGte, ">=", pos})
				i += 2
			} else {
				tokens = append(tokens, Token{TokenGt, ">", pos})
				i++
			}
			continue
		}

		// String literal
		if ch == '"' {
			tok, newI, err := lexString(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, tok)
			i = newI
			continue
		}

		// Backtick identifier
		if ch == '`' {
			tok, newI, err := lexBacktick(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, tok)
			i = newI
			continue
		}

		// Number
		if unicode.IsDigit(ch) {
			tok, newI, err := lexNumber(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, tok)
			i = newI
			continue
		}

		// Identifier or keyword
		if isIdentStart(ch) {
			tok, newI := lexIdent(runes, i)
			tokens = append(tokens, tok)
			i = newI
			continue
		}

		return nil, fmt.Errorf("unexpected character %q at position %d", ch, pos)
	}

	tokens = append(tokens, Token{TokenEOF, "", len(runes)})
	return tokens, nil
}

func isNegativeContext(tokens []Token) bool {
	if len(tokens) == 0 {
		return true
	}
	last := tokens[len(tokens)-1].Type
	switch last {
	case TokenLParen, TokenComma, TokenEquals, TokenPipe, TokenLBrace,
		TokenPlus, TokenMinus, TokenStar, TokenSlash,
		TokenEq, TokenNeq, TokenLt, TokenGt, TokenLte, TokenGte,
		TokenAnd, TokenOr, TokenNot:
		return true
	}
	return false
}

func lexString(runes []rune, start int) (Token, int, error) {
	i := start + 1 // skip opening quote
	var sb []rune
	for i < len(runes) {
		if runes[i] == '\\' && i+1 < len(runes) {
			switch runes[i+1] {
			case '"':
				sb = append(sb, '"')
			case '\\':
				sb = append(sb, '\\')
			case 'n':
				sb = append(sb, '\n')
			case 't':
				sb = append(sb, '\t')
			default:
				sb = append(sb, '\\', runes[i+1])
			}
			i += 2
			continue
		}
		if runes[i] == '"' {
			return Token{TokenString, string(sb), start}, i + 1, nil
		}
		sb = append(sb, runes[i])
		i++
	}
	return Token{}, 0, fmt.Errorf("unterminated string starting at position %d", start)
}

func lexBacktick(runes []rune, start int) (Token, int, error) {
	i := start + 1
	var sb []rune
	for i < len(runes) {
		if runes[i] == '`' {
			return Token{TokenBacktickIdent, string(sb), start}, i + 1, nil
		}
		sb = append(sb, runes[i])
		i++
	}
	return Token{}, 0, fmt.Errorf("unterminated backtick identifier starting at position %d", start)
}

func lexNumber(runes []rune, start int) (Token, int, error) {
	i := start
	isFloat := false

	if i < len(runes) && runes[i] == '-' {
		i++
	}

	for i < len(runes) && unicode.IsDigit(runes[i]) {
		i++
	}

	if i < len(runes) && runes[i] == '.' {
		// Check it's not a file extension like "users.csv"
		if i+1 < len(runes) && unicode.IsDigit(runes[i+1]) {
			isFloat = true
			i++
			for i < len(runes) && unicode.IsDigit(runes[i]) {
				i++
			}
		}
	}

	val := string(runes[start:i])
	if isFloat {
		return Token{TokenFloat, val, start}, i, nil
	}
	return Token{TokenInt, val, start}, i, nil
}

func lexIdent(runes []rune, start int) (Token, int) {
	i := start
	for i < len(runes) && isIdentPart(runes[i]) {
		i++
	}
	val := string(runes[start:i])

	if tt, ok := keywords[val]; ok {
		return Token{tt, val, start}, i
	}
	return Token{TokenIdent, val, start}, i
}

func isIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isIdentPart(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}
