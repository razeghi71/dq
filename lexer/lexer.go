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

// Lexer is a stateful tokenizer that supports both normal tokenization
// via Next() and greedy filename scanning via ScanFilename().
type Lexer struct {
	runes   []rune
	pos     int
	prevSet bool
	prev    TokenType
}

// NewLexer creates a new Lexer for the given input string.
func NewLexer(input string) *Lexer {
	return &Lexer{runes: []rune(input)}
}

func (l *Lexer) emit(tok Token) Token {
	l.prevSet = true
	l.prev = tok.Type
	return tok
}

func (l *Lexer) isNegativeContext() bool {
	if !l.prevSet {
		return true
	}
	switch l.prev {
	case TokenLParen, TokenComma, TokenEquals, TokenPipe, TokenLBrace,
		TokenPlus, TokenMinus, TokenStar, TokenSlash,
		TokenEq, TokenNeq, TokenLt, TokenGt, TokenLte, TokenGte,
		TokenAnd, TokenOr, TokenNot:
		return true
	}
	return false
}

// Next returns the next token using normal tokenization rules.
func (l *Lexer) Next() (Token, error) {
	for l.pos < len(l.runes) {
		ch := l.runes[l.pos]

		// Skip whitespace
		if unicode.IsSpace(ch) {
			l.pos++
			continue
		}

		pos := l.pos
		switch ch {
		case '|':
			l.pos++
			return l.emit(Token{TokenPipe, "|", pos}), nil
		case '{':
			l.pos++
			return l.emit(Token{TokenLBrace, "{", pos}), nil
		case '}':
			l.pos++
			return l.emit(Token{TokenRBrace, "}", pos}), nil
		case '(':
			l.pos++
			return l.emit(Token{TokenLParen, "(", pos}), nil
		case ')':
			l.pos++
			return l.emit(Token{TokenRParen, ")", pos}), nil
		case ',':
			l.pos++
			return l.emit(Token{TokenComma, ",", pos}), nil
		case '.':
			l.pos++
			return l.emit(Token{TokenDot, ".", pos}), nil
		case '+':
			l.pos++
			return l.emit(Token{TokenPlus, "+", pos}), nil
		case '-':
			if l.pos+1 < len(l.runes) && unicode.IsDigit(l.runes[l.pos+1]) && l.isNegativeContext() {
				tok, newPos, err := lexNumber(l.runes, l.pos)
				if err != nil {
					return Token{}, err
				}
				l.pos = newPos
				return l.emit(tok), nil
			}
			l.pos++
			return l.emit(Token{TokenMinus, "-", pos}), nil
		case '*':
			l.pos++
			return l.emit(Token{TokenStar, "*", pos}), nil
		case '/':
			if l.pos+1 < len(l.runes) && l.runes[l.pos+1] == '/' {
				for l.pos < len(l.runes) && l.runes[l.pos] != '\n' {
					l.pos++
				}
				continue
			}
			l.pos++
			return l.emit(Token{TokenSlash, "/", pos}), nil
		case '=':
			if l.pos+1 < len(l.runes) && l.runes[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(Token{TokenEq, "==", pos}), nil
			}
			l.pos++
			return l.emit(Token{TokenEquals, "=", pos}), nil
		case '!':
			if l.pos+1 < len(l.runes) && l.runes[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(Token{TokenNeq, "!=", pos}), nil
			}
			return Token{}, fmt.Errorf("unexpected character '!' at position %d (did you mean '!='?)", pos)
		case '<':
			if l.pos+1 < len(l.runes) && l.runes[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(Token{TokenLte, "<=", pos}), nil
			}
			l.pos++
			return l.emit(Token{TokenLt, "<", pos}), nil
		case '>':
			if l.pos+1 < len(l.runes) && l.runes[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(Token{TokenGte, ">=", pos}), nil
			}
			l.pos++
			return l.emit(Token{TokenGt, ">", pos}), nil
		}

		// String literal
		if ch == '"' {
			tok, newPos, err := lexString(l.runes, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Backtick identifier
		if ch == '`' {
			tok, newPos, err := lexBacktick(l.runes, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Number
		if unicode.IsDigit(ch) {
			tok, newPos, err := lexNumber(l.runes, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Identifier or keyword
		if isIdentStart(ch) {
			tok, newPos := lexIdent(l.runes, l.pos)
			l.pos = newPos
			return l.emit(tok), nil
		}

		return Token{}, fmt.Errorf("unexpected character %q at position %d", ch, pos)
	}

	return Token{TokenEOF, "", len(l.runes)}, nil
}

// ScanFilename reads a filename token greedily. It consumes all characters
// that are not whitespace and not '|'. Quoted and backtick-quoted filenames
// are also supported.
func (l *Lexer) ScanFilename() (Token, error) {
	// Skip whitespace
	for l.pos < len(l.runes) && unicode.IsSpace(l.runes[l.pos]) {
		l.pos++
	}

	if l.pos >= len(l.runes) {
		return Token{TokenEOF, "", l.pos}, nil
	}

	ch := l.runes[l.pos]

	// Quoted filename
	if ch == '"' {
		tok, newPos, err := lexString(l.runes, l.pos)
		if err != nil {
			return Token{}, err
		}
		l.pos = newPos
		l.prevSet = true
		l.prev = TokenIdent
		return Token{TokenIdent, tok.Val, tok.Pos}, nil
	}

	// Backtick-quoted filename
	if ch == '`' {
		tok, newPos, err := lexBacktick(l.runes, l.pos)
		if err != nil {
			return Token{}, err
		}
		l.pos = newPos
		l.prevSet = true
		l.prev = TokenIdent
		return Token{TokenIdent, tok.Val, tok.Pos}, nil
	}

	// Unquoted: consume all non-whitespace, non-pipe characters
	start := l.pos
	for l.pos < len(l.runes) && !unicode.IsSpace(l.runes[l.pos]) && l.runes[l.pos] != '|' {
		l.pos++
	}

	val := string(l.runes[start:l.pos])
	l.prevSet = true
	l.prev = TokenIdent
	return Token{TokenIdent, val, start}, nil
}

// Lex tokenizes the input string into a slice of Tokens.
// It is a convenience wrapper around the streaming Lexer.
func Lex(input string) ([]Token, error) {
	l := NewLexer(input)
	var tokens []Token
	for {
		tok, err := l.Next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}
	return tokens, nil
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
