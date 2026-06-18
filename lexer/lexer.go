package lexer

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// TokenType represents the type of a lexical token.
type TokenType uint8

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
	TokenWith  // with

	// Literals
	TokenInt    // integer literal
	TokenFloat  // float literal
	TokenString // "string literal"

	// Identifiers
	TokenIdent         // plain identifier (column name, op name)
	TokenBacktickIdent // `identifier with spaces`
	TokenStdin         // - (stdin source sentinel)

	// End
	TokenEOF
)

var tokenNames = map[TokenType]string{
	TokenPipe: "|", TokenLBrace: "{", TokenRBrace: "}", TokenLParen: "(", TokenRParen: ")",
	TokenComma: ",", TokenEquals: "=", TokenDot: ".",
	TokenPlus: "+", TokenMinus: "-", TokenStar: "*", TokenSlash: "/",
	TokenEq: "==", TokenNeq: "!=", TokenLt: "<", TokenGt: ">", TokenLte: "<=", TokenGte: ">=",
	TokenAnd: "and", TokenOr: "or", TokenNot: "not", TokenIs: "is",
	TokenTrue: "true", TokenFalse: "false", TokenNull: "null", TokenAs: "as", TokenWith: "with",
	TokenInt: "INT", TokenFloat: "FLOAT", TokenString: "STRING",
	TokenIdent: "IDENT", TokenBacktickIdent: "BACKTICK_IDENT", TokenStdin: "STDIN", TokenEOF: "EOF",
}

func (t TokenType) String() string {
	if s, ok := tokenNames[t]; ok {
		return s
	}
	return fmt.Sprintf("Token(%d)", int(t))
}

// Token represents a single lexical token.
type Token struct {
	Val  string
	Pos  int    // byte offset in original input
	End  uint32 // exclusive byte offset in original input
	Type TokenType
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
	"with":  TokenWith,
}

// Lexer is a stateful tokenizer that supports both normal tokenization
// via Next() and greedy source scanning via ScanSource().
type Lexer struct {
	input   string
	ascii   bool
	pos     int
	prevSet bool
	prev    TokenType
}

// NewLexer creates a new Lexer for the given input string.
func NewLexer(input string) *Lexer {
	return &Lexer{input: input, ascii: isASCIIInput(input)}
}

func isASCIIInput(input string) bool {
	for i := 0; i < len(input); i++ {
		if input[i] >= 0x80 {
			return false
		}
	}
	return true
}

func (l *Lexer) emit(tok Token) Token {
	l.prevSet = true
	l.prev = tok.Type
	return tok
}

func asciiToken(tt TokenType, val string, start, end int) Token {
	return Token{Type: tt, Val: val, Pos: start, End: uint32(end)}
}

func asciiIsSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func asciiIsDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func asciiIsIdentStart(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func asciiIsIdentPart(ch byte) bool {
	return asciiIsIdentStart(ch) || asciiIsDigit(ch)
}

func (l *Lexer) isNegativeContext() bool {
	if !l.prevSet {
		return true
	}
	switch l.prev {
	case TokenLParen, TokenComma, TokenEquals, TokenPipe, TokenLBrace,
		TokenPlus, TokenMinus, TokenStar, TokenSlash,
		TokenEq, TokenNeq, TokenLt, TokenGt, TokenLte, TokenGte,
		TokenAnd, TokenOr, TokenNot, TokenWith:
		return true
	}
	return false
}

// Next returns the next token using normal tokenization rules.
func (l *Lexer) Next() (Token, error) {
	if l.ascii {
		return l.nextASCII()
	}
	return l.nextUnicode()
}

func (l *Lexer) nextASCII() (Token, error) {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]

		// Skip whitespace
		if asciiIsSpace(ch) {
			l.pos++
			continue
		}

		pos := l.pos
		switch ch {
		case '|':
			l.pos++
			return l.emit(asciiToken(TokenPipe, "|", pos, l.pos)), nil
		case '{':
			l.pos++
			return l.emit(asciiToken(TokenLBrace, "{", pos, l.pos)), nil
		case '}':
			l.pos++
			return l.emit(asciiToken(TokenRBrace, "}", pos, l.pos)), nil
		case '(':
			l.pos++
			return l.emit(asciiToken(TokenLParen, "(", pos, l.pos)), nil
		case ')':
			l.pos++
			return l.emit(asciiToken(TokenRParen, ")", pos, l.pos)), nil
		case ',':
			l.pos++
			return l.emit(asciiToken(TokenComma, ",", pos, l.pos)), nil
		case '.':
			l.pos++
			return l.emit(asciiToken(TokenDot, ".", pos, l.pos)), nil
		case '+':
			l.pos++
			return l.emit(asciiToken(TokenPlus, "+", pos, l.pos)), nil
		case '-':
			if l.pos+1 < len(l.input) && asciiIsDigit(l.input[l.pos+1]) && l.isNegativeContext() {
				tok, newPos, err := lexASCIINumber(l.input, l.pos)
				if err != nil {
					return Token{}, err
				}
				l.pos = newPos
				return l.emit(tok), nil
			}
			l.pos++
			return l.emit(asciiToken(TokenMinus, "-", pos, l.pos)), nil
		case '*':
			l.pos++
			return l.emit(asciiToken(TokenStar, "*", pos, l.pos)), nil
		case '/':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '/' {
				for l.pos < len(l.input) && l.input[l.pos] != '\n' {
					l.pos++
				}
				continue
			}
			l.pos++
			return l.emit(asciiToken(TokenSlash, "/", pos, l.pos)), nil
		case '=':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenEq, "==", pos, l.pos)), nil
			}
			l.pos++
			return l.emit(asciiToken(TokenEquals, "=", pos, l.pos)), nil
		case '!':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenNeq, "!=", pos, l.pos)), nil
			}
			return Token{}, fmt.Errorf("unexpected character '!' at position %d (did you mean '!='?)", pos)
		case '<':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenLte, "<=", pos, l.pos)), nil
			}
			l.pos++
			return l.emit(asciiToken(TokenLt, "<", pos, l.pos)), nil
		case '>':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenGte, ">=", pos, l.pos)), nil
			}
			l.pos++
			return l.emit(asciiToken(TokenGt, ">", pos, l.pos)), nil
		}

		// String literal
		if ch == '"' {
			tok, newPos, err := lexASCIIString(l.input, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Backtick identifier
		if ch == '`' {
			tok, newPos, err := lexASCIIBacktick(l.input, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Number
		if asciiIsDigit(ch) {
			tok, newPos, err := lexASCIINumber(l.input, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Identifier or keyword
		if asciiIsIdentStart(ch) {
			tok, newPos := lexASCIIIdent(l.input, l.pos)
			l.pos = newPos
			return l.emit(tok), nil
		}

		return Token{}, fmt.Errorf("unexpected character %q at position %d", rune(ch), pos)
	}

	return asciiToken(TokenEOF, "", l.pos, l.pos), nil
}

func (l *Lexer) nextUnicode() (Token, error) {
	for l.pos < len(l.input) {
		ch, width := utf8.DecodeRuneInString(l.input[l.pos:])
		if ch == utf8.RuneError && width == 0 {
			break
		}

		// Skip whitespace
		if unicode.IsSpace(ch) {
			l.pos += width
			continue
		}

		pos := l.pos
		switch ch {
		case '|':
			l.pos += width
			return l.emit(asciiToken(TokenPipe, "|", pos, l.pos)), nil
		case '{':
			l.pos += width
			return l.emit(asciiToken(TokenLBrace, "{", pos, l.pos)), nil
		case '}':
			l.pos += width
			return l.emit(asciiToken(TokenRBrace, "}", pos, l.pos)), nil
		case '(':
			l.pos += width
			return l.emit(asciiToken(TokenLParen, "(", pos, l.pos)), nil
		case ')':
			l.pos += width
			return l.emit(asciiToken(TokenRParen, ")", pos, l.pos)), nil
		case ',':
			l.pos += width
			return l.emit(asciiToken(TokenComma, ",", pos, l.pos)), nil
		case '.':
			l.pos += width
			return l.emit(asciiToken(TokenDot, ".", pos, l.pos)), nil
		case '+':
			l.pos += width
			return l.emit(asciiToken(TokenPlus, "+", pos, l.pos)), nil
		case '-':
			if l.pos+1 < len(l.input) {
				next, _ := utf8.DecodeRuneInString(l.input[l.pos+1:])
				if unicode.IsDigit(next) && l.isNegativeContext() {
					tok, newPos, err := lexUnicodeNumber(l.input, l.pos)
					if err != nil {
						return Token{}, err
					}
					l.pos = newPos
					return l.emit(tok), nil
				}
			}
			l.pos += width
			return l.emit(asciiToken(TokenMinus, "-", pos, l.pos)), nil
		case '*':
			l.pos += width
			return l.emit(asciiToken(TokenStar, "*", pos, l.pos)), nil
		case '/':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '/' {
				for l.pos < len(l.input) && l.input[l.pos] != '\n' {
					l.pos++
				}
				continue
			}
			l.pos += width
			return l.emit(asciiToken(TokenSlash, "/", pos, l.pos)), nil
		case '=':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenEq, "==", pos, l.pos)), nil
			}
			l.pos += width
			return l.emit(asciiToken(TokenEquals, "=", pos, l.pos)), nil
		case '!':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenNeq, "!=", pos, l.pos)), nil
			}
			return Token{}, fmt.Errorf("unexpected character '!' at position %d (did you mean '!='?)", pos)
		case '<':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenLte, "<=", pos, l.pos)), nil
			}
			l.pos += width
			return l.emit(asciiToken(TokenLt, "<", pos, l.pos)), nil
		case '>':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.pos += 2
				return l.emit(asciiToken(TokenGte, ">=", pos, l.pos)), nil
			}
			l.pos += width
			return l.emit(asciiToken(TokenGt, ">", pos, l.pos)), nil
		}

		// String literal
		if ch == '"' {
			tok, newPos, err := lexASCIIString(l.input, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Backtick identifier
		if ch == '`' {
			tok, newPos, err := lexASCIIBacktick(l.input, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Number
		if unicode.IsDigit(ch) {
			tok, newPos, err := lexUnicodeNumber(l.input, l.pos)
			if err != nil {
				return Token{}, err
			}
			l.pos = newPos
			return l.emit(tok), nil
		}

		// Identifier or keyword
		if isIdentStart(ch) {
			tok, newPos := lexUnicodeIdent(l.input, l.pos)
			l.pos = newPos
			return l.emit(tok), nil
		}

		return Token{}, fmt.Errorf("unexpected character %q at position %d", ch, pos)
	}

	return asciiToken(TokenEOF, "", l.pos, l.pos), nil
}

// ScanSource reads the query source token: a lone '-' (stdin), a filename,
// or a quoted/backtick-quoted path. Unquoted filenames consume all characters
// that are not whitespace and not '|'.
func (l *Lexer) ScanSource() (Token, error) {
	if l.ascii {
		return l.scanSourceASCII()
	}
	return l.scanSourceUnicode()
}

func (l *Lexer) scanSourceASCII() (Token, error) {
	// Skip whitespace
	for l.pos < len(l.input) && asciiIsSpace(l.input[l.pos]) {
		l.pos++
	}

	if l.pos >= len(l.input) {
		return asciiToken(TokenEOF, "", l.pos, l.pos), nil
	}

	ch := l.input[l.pos]

	// Quoted filename
	if ch == '"' {
		tok, newPos, err := lexASCIIString(l.input, l.pos)
		if err != nil {
			return Token{}, err
		}
		l.pos = newPos
		l.prevSet = true
		l.prev = TokenIdent
		return Token{Type: TokenIdent, Val: tok.Val, Pos: tok.Pos, End: tok.End}, nil
	}

	// Backtick-quoted filename
	if ch == '`' {
		tok, newPos, err := lexASCIIBacktick(l.input, l.pos)
		if err != nil {
			return Token{}, err
		}
		l.pos = newPos
		l.prevSet = true
		l.prev = TokenIdent
		return Token{Type: TokenIdent, Val: tok.Val, Pos: tok.Pos, End: tok.End}, nil
	}

	// Lone '-' means stdin (not a hyphenated filename prefix).
	if ch == '-' {
		next := l.pos + 1
		if next >= len(l.input) || asciiIsSpace(l.input[next]) || l.input[next] == '|' {
			pos := l.pos
			l.pos = next
			l.prevSet = true
			l.prev = TokenStdin
			return asciiToken(TokenStdin, "-", pos, l.pos), nil
		}
	}

	// Unquoted: consume all non-whitespace, non-pipe characters
	start := l.pos
	for l.pos < len(l.input) && !asciiIsSpace(l.input[l.pos]) && l.input[l.pos] != '|' {
		l.pos++
	}

	val := l.input[start:l.pos]
	l.prevSet = true
	l.prev = TokenIdent
	return asciiToken(TokenIdent, val, start, l.pos), nil
}

func (l *Lexer) scanSourceUnicode() (Token, error) {
	// Skip whitespace
	for l.pos < len(l.input) {
		ch, width := utf8.DecodeRuneInString(l.input[l.pos:])
		if ch == utf8.RuneError && width == 0 {
			break
		}
		if !unicode.IsSpace(ch) {
			break
		}
		l.pos += width
	}

	if l.pos >= len(l.input) {
		return asciiToken(TokenEOF, "", l.pos, l.pos), nil
	}

	ch, _ := utf8.DecodeRuneInString(l.input[l.pos:])

	// Quoted filename
	if ch == '"' {
		tok, newPos, err := lexASCIIString(l.input, l.pos)
		if err != nil {
			return Token{}, err
		}
		l.pos = newPos
		l.prevSet = true
		l.prev = TokenIdent
		return Token{Type: TokenIdent, Val: tok.Val, Pos: tok.Pos, End: tok.End}, nil
	}

	// Backtick-quoted filename
	if ch == '`' {
		tok, newPos, err := lexASCIIBacktick(l.input, l.pos)
		if err != nil {
			return Token{}, err
		}
		l.pos = newPos
		l.prevSet = true
		l.prev = TokenIdent
		return Token{Type: TokenIdent, Val: tok.Val, Pos: tok.Pos, End: tok.End}, nil
	}

	// Lone '-' means stdin (not a hyphenated filename prefix).
	if ch == '-' {
		next := l.pos + 1
		if next >= len(l.input) || l.input[next] == '|' {
			pos := l.pos
			l.pos = next
			l.prevSet = true
			l.prev = TokenStdin
			return asciiToken(TokenStdin, "-", pos, l.pos), nil
		}
		nextRune, _ := utf8.DecodeRuneInString(l.input[next:])
		if unicode.IsSpace(nextRune) {
			pos := l.pos
			l.pos = next
			l.prevSet = true
			l.prev = TokenStdin
			return asciiToken(TokenStdin, "-", pos, l.pos), nil
		}
	}

	// Unquoted: consume all non-whitespace, non-pipe characters
	start := l.pos
	for l.pos < len(l.input) {
		ch, width := utf8.DecodeRuneInString(l.input[l.pos:])
		if ch == utf8.RuneError && width == 0 {
			break
		}
		if unicode.IsSpace(ch) || ch == '|' {
			break
		}
		l.pos += width
	}

	val := l.input[start:l.pos]
	l.prevSet = true
	l.prev = TokenIdent
	return asciiToken(TokenIdent, val, start, l.pos), nil
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

func lexASCIIString(input string, start int) (Token, int, error) {
	i := start + 1 // skip opening quote
	segmentStart := i
	var sb []byte
	for i < len(input) {
		if input[i] == '\\' && i+1 < len(input) {
			if sb == nil {
				sb = make([]byte, 0, len(input)-start)
			}
			sb = append(sb, input[segmentStart:i]...)
			switch input[i+1] {
			case '"':
				sb = append(sb, '"')
			case '\\':
				sb = append(sb, '\\')
			case 'n':
				sb = append(sb, '\n')
			case 't':
				sb = append(sb, '\t')
			default:
				sb = append(sb, '\\', input[i+1])
			}
			i += 2
			segmentStart = i
			continue
		}
		if input[i] == '"' {
			val := input[segmentStart:i]
			if sb != nil {
				sb = append(sb, input[segmentStart:i]...)
				val = string(sb)
			}
			return asciiToken(TokenString, val, start, i+1), i + 1, nil
		}
		i++
	}
	return Token{}, 0, fmt.Errorf("unterminated string starting at position %d", start)
}

func lexASCIIBacktick(input string, start int) (Token, int, error) {
	i := start + 1
	for i < len(input) {
		if input[i] == '`' {
			return asciiToken(TokenBacktickIdent, input[start+1:i], start, i+1), i + 1, nil
		}
		i++
	}
	return Token{}, 0, fmt.Errorf("unterminated backtick identifier starting at position %d", start)
}

func lexASCIINumber(input string, start int) (Token, int, error) {
	i := start
	isFloat := false

	if i < len(input) && input[i] == '-' {
		i++
	}

	for i < len(input) && asciiIsDigit(input[i]) {
		i++
	}

	if i < len(input) && input[i] == '.' {
		// Check it's not a file extension like "users.csv"
		if i+1 < len(input) && asciiIsDigit(input[i+1]) {
			isFloat = true
			i++
			for i < len(input) && asciiIsDigit(input[i]) {
				i++
			}
		}
	}

	if isFloat {
		return asciiToken(TokenFloat, input[start:i], start, i), i, nil
	}
	return asciiToken(TokenInt, input[start:i], start, i), i, nil
}

func lexUnicodeNumber(input string, start int) (Token, int, error) {
	i := start
	isFloat := false

	if i < len(input) && input[i] == '-' {
		i++
	}

	for i < len(input) {
		ch, width := utf8.DecodeRuneInString(input[i:])
		if ch == utf8.RuneError && width == 0 {
			break
		}
		if !unicode.IsDigit(ch) {
			break
		}
		i += width
	}

	if i < len(input) && input[i] == '.' {
		if i+1 < len(input) {
			ch, width := utf8.DecodeRuneInString(input[i+1:])
			if unicode.IsDigit(ch) {
				isFloat = true
				i += 1 + width
				for i < len(input) {
					ch, width := utf8.DecodeRuneInString(input[i:])
					if ch == utf8.RuneError && width == 0 {
						break
					}
					if !unicode.IsDigit(ch) {
						break
					}
					i += width
				}
			}
		}
	}

	if isFloat {
		return asciiToken(TokenFloat, input[start:i], start, i), i, nil
	}
	return asciiToken(TokenInt, input[start:i], start, i), i, nil
}

func lexASCIIIdent(input string, start int) (Token, int) {
	i := start
	for i < len(input) && asciiIsIdentPart(input[i]) {
		i++
	}
	val := input[start:i]

	if tt, ok := keywords[val]; ok {
		return asciiToken(tt, val, start, i), i
	}
	return asciiToken(TokenIdent, val, start, i), i
}

func isIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isIdentPart(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}

func lexUnicodeIdent(input string, start int) (Token, int) {
	i := start
	for i < len(input) {
		ch, width := utf8.DecodeRuneInString(input[i:])
		if ch == utf8.RuneError && width == 0 {
			break
		}
		if !isIdentPart(ch) {
			break
		}
		i += width
	}
	val := input[start:i]

	if tt, ok := keywords[val]; ok {
		return asciiToken(tt, val, start, i), i
	}
	return asciiToken(TokenIdent, val, start, i), i
}
