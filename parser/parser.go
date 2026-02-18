package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/lexer"
)

// Parser converts a token stream into an AST.
type Parser struct {
	tokens []lexer.Token
	pos    int
}

// Parse parses a full query string into a Query AST.
func Parse(input string) (*ast.Query, error) {
	tokens, err := lexer.Lex(input)
	if err != nil {
		return nil, fmt.Errorf("lex error: %w", err)
	}
	p := &Parser{tokens: tokens, pos: 0}
	return p.parseQuery()
}

func (p *Parser) peek() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Type: lexer.TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) advance() lexer.Token {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *Parser) expect(tt lexer.TokenType) (lexer.Token, error) {
	tok := p.advance()
	if tok.Type != tt {
		return tok, fmt.Errorf("expected %s, got %s (%q) at position %d", tt, tok.Type, tok.Val, tok.Pos)
	}
	return tok, nil
}

func (p *Parser) parseQuery() (*ast.Query, error) {
	// Parse source: filename (could contain dots like "users.csv")
	source, err := p.parseSource()
	if err != nil {
		return nil, err
	}

	var ops []ast.Op
	for p.peek().Type == lexer.TokenPipe {
		p.advance() // consume |
		op, err := p.parseOp()
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}

	if p.peek().Type != lexer.TokenEOF {
		return nil, fmt.Errorf("unexpected token %s (%q) at position %d", p.peek().Type, p.peek().Val, p.peek().Pos)
	}

	return &ast.Query{Source: source, Ops: ops}, nil
}

func (p *Parser) parseSource() (*ast.SourceOp, error) {
	// Filename can be like "path/to/users.csv" which tokenizes as
	// IDENT SLASH IDENT SLASH IDENT DOT IDENT
	// Or a quoted string: "my file.csv"
	tok := p.advance()
	if tok.Type == lexer.TokenString {
		return &ast.SourceOp{Filename: tok.Val}, nil
	}
	if tok.Type != lexer.TokenIdent && tok.Type != lexer.TokenBacktickIdent {
		return nil, fmt.Errorf("expected filename, got %s (%q) at position %d", tok.Type, tok.Val, tok.Pos)
	}

	filename := tok.Val

	// Consume subsequent /ident and .ident sequences to form full file path
	for p.peek().Type == lexer.TokenDot || p.peek().Type == lexer.TokenSlash {
		sep := p.advance()
		next := p.advance()
		if next.Type != lexer.TokenIdent && next.Type != lexer.TokenInt {
			return nil, fmt.Errorf("expected path component after %q, got %s at position %d", sep.Val, next.Type, next.Pos)
		}
		filename += sep.Val + next.Val
	}

	return &ast.SourceOp{Filename: filename}, nil
}

func (p *Parser) parseOp() (ast.Op, error) {
	tok := p.peek()
	if tok.Type != lexer.TokenIdent {
		return nil, fmt.Errorf("expected operation name, got %s (%q) at position %d", tok.Type, tok.Val, tok.Pos)
	}

	switch tok.Val {
	case "head":
		return p.parseHead()
	case "tail":
		return p.parseTail()
	case "sorta":
		return p.parseSortAsc()
	case "sortd":
		return p.parseSortDesc()
	case "select":
		return p.parseSelect()
	case "filter":
		return p.parseFilter()
	case "group":
		return p.parseGroup()
	case "transform":
		return p.parseTransform()
	case "reduce":
		return p.parseReduce()
	case "count":
		return p.parseCount()
	case "distinct":
		return p.parseDistinct()
	case "rename":
		return p.parseRename()
	case "remove":
		return p.parseRemove()
	default:
		return nil, fmt.Errorf("unknown operation %q at position %d", tok.Val, tok.Pos)
	}
}

func (p *Parser) parseHead() (ast.Op, error) {
	p.advance() // consume "head"
	n, err := p.parseInt()
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	return &ast.HeadOp{N: n}, nil
}

func (p *Parser) parseTail() (ast.Op, error) {
	p.advance() // consume "tail"
	n, err := p.parseInt()
	if err != nil {
		return nil, fmt.Errorf("tail: %w", err)
	}
	return &ast.TailOp{N: n}, nil
}

func (p *Parser) parseSortAsc() (ast.Op, error) {
	p.advance() // consume "sorta"
	cols, err := p.parseColumnList()
	if err != nil {
		return nil, fmt.Errorf("sorta: %w", err)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("sorta: expected at least one column")
	}
	return &ast.SortAscOp{Columns: cols}, nil
}

func (p *Parser) parseSortDesc() (ast.Op, error) {
	p.advance() // consume "sortd"
	cols, err := p.parseColumnList()
	if err != nil {
		return nil, fmt.Errorf("sortd: %w", err)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("sortd: expected at least one column")
	}
	return &ast.SortDescOp{Columns: cols}, nil
}

func (p *Parser) parseSelect() (ast.Op, error) {
	p.advance() // consume "select"
	cols, err := p.parseColumnList()
	if err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("select: expected at least one column")
	}
	return &ast.SelectOp{Columns: cols}, nil
}

func (p *Parser) parseFilter() (ast.Op, error) {
	p.advance() // consume "filter"
	if _, err := p.expect(lexer.TokenLBrace); err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	if _, err := p.expect(lexer.TokenRBrace); err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	return &ast.FilterOp{Expr: expr}, nil
}

func (p *Parser) parseGroup() (ast.Op, error) {
	p.advance() // consume "group"
	cols, err := p.parseColumnListUntilAs()
	if err != nil {
		return nil, fmt.Errorf("group: %w", err)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("group: expected at least one column")
	}

	nestedName := "grouped"
	if p.peek().Type == lexer.TokenAs {
		p.advance() // consume "as"
		nameTok := p.advance()
		if nameTok.Type != lexer.TokenIdent && nameTok.Type != lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("group: expected nested name after 'as', got %s", nameTok.Type)
		}
		nestedName = nameTok.Val
	}

	return &ast.GroupOp{Columns: cols, NestedName: nestedName}, nil
}

func (p *Parser) parseTransform() (ast.Op, error) {
	p.advance() // consume "transform"
	assignments, err := p.parseAssignments()
	if err != nil {
		return nil, fmt.Errorf("transform: %w", err)
	}
	return &ast.TransformOp{Assignments: assignments}, nil
}

func (p *Parser) parseReduce() (ast.Op, error) {
	p.advance() // consume "reduce"

	// The nested name is optional. We need to look ahead to determine
	// if the first identifier is a nested name or the start of assignments.
	// If we see: IDENT IDENT = expr  -> first IDENT is nested name
	// If we see: IDENT = expr        -> no nested name, first IDENT is column in assignment
	nestedName := "grouped"

	if p.peek().Type == lexer.TokenIdent {
		// Look ahead: is the token after the ident an "=" or another ident?
		saved := p.pos
		firstIdent := p.advance()

		if p.peek().Type == lexer.TokenEquals {
			// This is "col = expr" pattern, no nested name
			p.pos = saved // rewind
		} else if p.peek().Type == lexer.TokenIdent || p.peek().Type == lexer.TokenBacktickIdent {
			// Could be "nested_name col = expr"
			// Check if the second ident is followed by =
			secondPos := p.pos
			p.advance() // consume second ident
			if p.peek().Type == lexer.TokenEquals {
				// Yes: first ident is nested name
				nestedName = firstIdent.Val
				p.pos = secondPos // rewind to second ident
			} else {
				// Not an assignment pattern, rewind everything
				p.pos = saved
			}
		} else {
			// Just a single ident not followed by = or ident; rewind
			p.pos = saved
		}
	}

	assignments, err := p.parseAssignments()
	if err != nil {
		return nil, fmt.Errorf("reduce: %w", err)
	}
	return &ast.ReduceOp{NestedName: nestedName, Assignments: assignments}, nil
}

func (p *Parser) parseCount() (ast.Op, error) {
	p.advance() // consume "count"
	return &ast.CountOp{}, nil
}

func (p *Parser) parseDistinct() (ast.Op, error) {
	p.advance() // consume "distinct"
	cols, err := p.parseColumnList()
	if err != nil {
		return nil, fmt.Errorf("distinct: %w", err)
	}
	return &ast.DistinctOp{Columns: cols}, nil
}

func (p *Parser) parseRename() (ast.Op, error) {
	p.advance() // consume "rename"
	var pairs []ast.RenamePair
	for p.peek().Type == lexer.TokenIdent || p.peek().Type == lexer.TokenBacktickIdent {
		oldTok := p.advance()
		newTok := p.advance()
		if newTok.Type != lexer.TokenIdent && newTok.Type != lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("rename: expected new column name, got %s (%q)", newTok.Type, newTok.Val)
		}
		pairs = append(pairs, ast.RenamePair{Old: oldTok.Val, New: newTok.Val})
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("rename: expected at least one old/new pair")
	}
	return &ast.RenameOp{Pairs: pairs}, nil
}

func (p *Parser) parseRemove() (ast.Op, error) {
	p.advance() // consume "remove"
	cols, err := p.parseColumnList()
	if err != nil {
		return nil, fmt.Errorf("remove: %w", err)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("remove: expected at least one column")
	}
	return &ast.RemoveOp{Columns: cols}, nil
}

// --- Helpers ---

func (p *Parser) parseInt() (int, error) {
	tok := p.advance()
	if tok.Type != lexer.TokenInt {
		return 0, fmt.Errorf("expected integer, got %s (%q) at position %d", tok.Type, tok.Val, tok.Pos)
	}
	n, err := strconv.Atoi(tok.Val)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q: %w", tok.Val, err)
	}
	return n, nil
}

// parseColumnList reads identifiers until we hit something that isn't a column name.
func (p *Parser) parseColumnList() ([]string, error) {
	var cols []string
	for p.peek().Type == lexer.TokenIdent || p.peek().Type == lexer.TokenBacktickIdent {
		tok := p.advance()
		cols = append(cols, tok.Val)
	}
	return cols, nil
}

// parseColumnListUntilAs reads identifiers but stops at "as" keyword.
func (p *Parser) parseColumnListUntilAs() ([]string, error) {
	var cols []string
	for p.peek().Type == lexer.TokenIdent || p.peek().Type == lexer.TokenBacktickIdent {
		if p.peek().Type == lexer.TokenAs {
			break
		}
		tok := p.advance()
		cols = append(cols, tok.Val)
	}
	return cols, nil
}

// parseAssignments parses comma-separated "col = expr" assignments.
func (p *Parser) parseAssignments() ([]ast.Assignment, error) {
	var assignments []ast.Assignment

	for {
		colTok := p.advance()
		if colTok.Type != lexer.TokenIdent && colTok.Type != lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("expected column name in assignment, got %s (%q)", colTok.Type, colTok.Val)
		}

		if _, err := p.expect(lexer.TokenEquals); err != nil {
			return nil, fmt.Errorf("expected '=' after column %q: %w", colTok.Val, err)
		}

		expr, err := p.parseExpr()
		if err != nil {
			return nil, fmt.Errorf("in assignment for %q: %w", colTok.Val, err)
		}

		assignments = append(assignments, ast.Assignment{Column: colTok.Val, Expr: expr})

		if p.peek().Type != lexer.TokenComma {
			break
		}
		p.advance() // consume comma
	}

	return assignments, nil
}

// --- Expression parsing (Pratt parser / precedence climbing) ---

// Precedence levels
const (
	precOr    = 1
	precAnd   = 2
	precComp  = 3
	precAdd   = 4
	precMul   = 5
	precUnary = 6
)

func (p *Parser) parseExpr() (ast.Expr, error) {
	return p.parseExprPrec(precOr)
}

func (p *Parser) parseExprPrec(minPrec int) (ast.Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		op, prec, ok := p.peekBinaryOp()
		if !ok || prec < minPrec {
			break
		}
		p.advanceBinaryOp(op) // consume the operator token(s)

		right, err := p.parseExprPrec(prec + 1) // left-associative
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{Op: op, Left: left, Right: right}
	}

	// Handle "is [not] null"
	if p.peek().Type == lexer.TokenIs {
		p.advance() // consume "is"
		negated := false
		if p.peek().Type == lexer.TokenNot {
			p.advance() // consume "not"
			negated = true
		}
		if _, err := p.expect(lexer.TokenNull); err != nil {
			return nil, fmt.Errorf("expected 'null' after 'is%s'", func() string {
				if negated {
					return " not"
				}
				return ""
			}())
		}
		left = &ast.IsNullExpr{Operand: left, Negated: negated}
	}

	return left, nil
}

func (p *Parser) peekBinaryOp() (string, int, bool) {
	tok := p.peek()
	switch tok.Type {
	case lexer.TokenOr:
		return "or", precOr, true
	case lexer.TokenAnd:
		return "and", precAnd, true
	case lexer.TokenEq:
		return "==", precComp, true
	case lexer.TokenNeq:
		return "!=", precComp, true
	case lexer.TokenLt:
		return "<", precComp, true
	case lexer.TokenGt:
		return ">", precComp, true
	case lexer.TokenLte:
		return "<=", precComp, true
	case lexer.TokenGte:
		return ">=", precComp, true
	case lexer.TokenPlus:
		return "+", precAdd, true
	case lexer.TokenMinus:
		return "-", precAdd, true
	case lexer.TokenStar:
		return "*", precMul, true
	case lexer.TokenSlash:
		return "/", precMul, true
	}
	return "", 0, false
}

func (p *Parser) advanceBinaryOp(op string) {
	p.advance()
	_ = op
}

func (p *Parser) parseUnary() (ast.Expr, error) {
	if p.peek().Type == lexer.TokenNot {
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: "not", Operand: operand}, nil
	}
	if p.peek().Type == lexer.TokenMinus {
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: "-", Operand: operand}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (ast.Expr, error) {
	tok := p.peek()

	switch tok.Type {
	case lexer.TokenInt:
		p.advance()
		v, err := strconv.ParseInt(tok.Val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q: %w", tok.Val, err)
		}
		return &ast.LiteralExpr{Kind: "int", Int: v}, nil

	case lexer.TokenFloat:
		p.advance()
		v, err := strconv.ParseFloat(tok.Val, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q: %w", tok.Val, err)
		}
		return &ast.LiteralExpr{Kind: "float", Float: v}, nil

	case lexer.TokenString:
		p.advance()
		return &ast.LiteralExpr{Kind: "string", Str: tok.Val}, nil

	case lexer.TokenTrue:
		p.advance()
		return &ast.LiteralExpr{Kind: "bool", Bool: true}, nil

	case lexer.TokenFalse:
		p.advance()
		return &ast.LiteralExpr{Kind: "bool", Bool: false}, nil

	case lexer.TokenNull:
		p.advance()
		return &ast.LiteralExpr{Kind: "null"}, nil

	case lexer.TokenBacktickIdent:
		p.advance()
		return &ast.ColumnExpr{Name: tok.Val}, nil

	case lexer.TokenIdent:
		p.advance()
		// Check if it's a function call
		if p.peek().Type == lexer.TokenLParen {
			return p.parseFuncCall(tok.Val)
		}
		return &ast.ColumnExpr{Name: tok.Val}, nil

	case lexer.TokenLParen:
		p.advance() // consume (
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokenRParen); err != nil {
			return nil, err
		}
		return expr, nil

	default:
		return nil, fmt.Errorf("unexpected token %s (%q) at position %d in expression", tok.Type, tok.Val, tok.Pos)
	}
}

func (p *Parser) parseFuncCall(name string) (ast.Expr, error) {
	p.advance() // consume (
	name = strings.ToLower(name)

	var args []ast.Expr
	if p.peek().Type != lexer.TokenRParen {
		for {
			arg, err := p.parseExpr()
			if err != nil {
				return nil, fmt.Errorf("in function %s: %w", name, err)
			}
			args = append(args, arg)
			if p.peek().Type != lexer.TokenComma {
				break
			}
			p.advance() // consume comma
		}
	}

	if _, err := p.expect(lexer.TokenRParen); err != nil {
		return nil, fmt.Errorf("in function %s: %w", name, err)
	}

	return &ast.FuncCallExpr{Name: name, Args: args}, nil
}
