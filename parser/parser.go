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
	lexer  *lexer.Lexer
	buf    []lexer.Token // lookahead buffer
	lexErr error         // first lexer error encountered
}

// Parse parses a full query string into a Query AST.
func Parse(input string) (*ast.Query, error) {
	p := &Parser{lexer: lexer.NewLexer(input)}
	q, err := p.parseQuery()
	if err != nil {
		if p.lexErr != nil {
			return nil, fmt.Errorf("lex error: %w", p.lexErr)
		}
		return nil, err
	}
	return q, nil
}

func (p *Parser) fillBuf(n int) {
	for len(p.buf) <= n && p.lexErr == nil {
		tok, err := p.lexer.Next()
		if err != nil {
			p.lexErr = err
			return
		}
		p.buf = append(p.buf, tok)
	}
}

func (p *Parser) peek() lexer.Token {
	return p.peekAt(0)
}

func (p *Parser) peekAt(n int) lexer.Token {
	p.fillBuf(n)
	if n < len(p.buf) {
		return p.buf[n]
	}
	return lexer.Token{Type: lexer.TokenEOF}
}

func (p *Parser) advance() lexer.Token {
	p.fillBuf(0)
	if len(p.buf) > 0 {
		tok := p.buf[0]
		p.buf = p.buf[1:]
		return tok
	}
	return lexer.Token{Type: lexer.TokenEOF}
}

func (p *Parser) expect(tt lexer.TokenType) (lexer.Token, error) {
	tok := p.advance()
	if tok.Type != tt {
		return tok, fmt.Errorf("expected %s, got %s (%q) at position %d", tt, tok.Type, tok.Val, tok.Pos)
	}
	return tok, nil
}

func (p *Parser) parseQuery() (*ast.Query, error) {
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
	// Clear any buffered tokens
	p.buf = nil
	tok, err := p.lexer.ScanSource()
	if err != nil {
		return nil, err
	}
	var filename string
	switch tok.Type {
	case lexer.TokenStdin:
		filename = "-"
	case lexer.TokenIdent:
		if tok.Val == "" {
			return nil, fmt.Errorf("expected filename at position %d", tok.Pos)
		}
		filename = tok.Val
	default:
		return nil, fmt.Errorf("expected filename at position %d", tok.Pos)
	}

	load, err := p.parseOptionalWithClause()
	if err != nil {
		return nil, err
	}
	if err := ast.ValidateLoadOptionsForFilename(filename, load); err != nil {
		return nil, err
	}
	return &ast.SourceOp{Filename: filename, Load: load}, nil
}

var loadOptionKeys = map[string]bool{
	"format": true,
	"header": true,
	"delim":  true,
}

func (p *Parser) parseOptionalWithClause() (ast.LoadOptions, error) {
	if p.peek().Type != lexer.TokenWith {
		return ast.LoadOptions{}, nil
	}
	return p.parseWithClause()
}

func (p *Parser) parseWithClause() (ast.LoadOptions, error) {
	if _, err := p.expect(lexer.TokenWith); err != nil {
		return ast.LoadOptions{}, err
	}

	var opts ast.LoadOptions
	seen := make(map[string]bool)
	for {
		keyTok, err := p.expect(lexer.TokenIdent)
		if err != nil {
			return ast.LoadOptions{}, fmt.Errorf("with: expected option name: %w", err)
		}
		if !loadOptionKeys[keyTok.Val] {
			return ast.LoadOptions{}, fmt.Errorf("with: unknown load option %q", keyTok.Val)
		}
		if seen[keyTok.Val] {
			return ast.LoadOptions{}, fmt.Errorf("with: duplicate load option %q", keyTok.Val)
		}
		seen[keyTok.Val] = true

		if _, err := p.expect(lexer.TokenEquals); err != nil {
			return ast.LoadOptions{}, fmt.Errorf("with: expected '=' after %q: %w", keyTok.Val, err)
		}

		valTok := p.advance()
		switch keyTok.Val {
		case "format":
			if valTok.Type != lexer.TokenIdent {
				return ast.LoadOptions{}, fmt.Errorf("with: format value must be an identifier, got %s", valTok.Type)
			}
			opts.Format = strings.ToLower(valTok.Val)
		case "header":
			switch valTok.Type {
			case lexer.TokenTrue:
				v := true
				opts.Header = &v
			case lexer.TokenFalse:
				v := false
				opts.Header = &v
			default:
				return ast.LoadOptions{}, fmt.Errorf("with: header value must be true or false, got %s", valTok.Type)
			}
		case "delim":
			if valTok.Type != lexer.TokenString {
				return ast.LoadOptions{}, fmt.Errorf("with: delim value must be a string, got %s", valTok.Type)
			}
			if valTok.Val == "" {
				return ast.LoadOptions{}, fmt.Errorf("with: delim cannot be empty")
			}
			opts.Delim = valTok.Val
		}

		if p.peek().Type != lexer.TokenComma {
			break
		}
		p.advance()
	}

	if err := ast.ValidateLoadOptions(opts); err != nil {
		return ast.LoadOptions{}, err
	}
	return opts, nil
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
	case "sort":
		return p.parseSort()
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
	case "join":
		return p.parseJoin()
	default:
		return nil, fmt.Errorf("unknown operation %q at position %d", tok.Val, tok.Pos)
	}
}

func (p *Parser) parseHead() (ast.Op, error) {
	p.advance() // consume "head"
	n := 10
	if p.peek().Type == lexer.TokenInt {
		var err error
		n, err = p.parseInt()
		if err != nil {
			return nil, fmt.Errorf("head: %w", err)
		}
	}
	return &ast.HeadOp{N: n}, nil
}

func (p *Parser) parseTail() (ast.Op, error) {
	p.advance() // consume "tail"
	n := 10
	if p.peek().Type == lexer.TokenInt {
		var err error
		n, err = p.parseInt()
		if err != nil {
			return nil, fmt.Errorf("tail: %w", err)
		}
	}
	return &ast.TailOp{N: n}, nil
}

func (p *Parser) parseSort() (ast.Op, error) {
	p.advance() // consume "sort"
	var keys []ast.SortKey
	for {
		desc := false
		if p.peek().Type == lexer.TokenMinus {
			p.advance()
			desc = true
		}
		t := p.peek()
		if t.Type != lexer.TokenIdent && t.Type != lexer.TokenBacktickIdent {
			if desc {
				return nil, fmt.Errorf("sort: expected column name after '-'")
			}
			break
		}
		path, err := p.parseOneColumnPath()
		if err != nil {
			return nil, fmt.Errorf("sort: %w", err)
		}
		keys = append(keys, ast.SortKey{Path: path, Desc: desc})

		if p.peek().Type == lexer.TokenComma {
			p.advance()
			if p.peek().Type != lexer.TokenIdent && p.peek().Type != lexer.TokenBacktickIdent && p.peek().Type != lexer.TokenMinus {
				return nil, fmt.Errorf("sort: expected column name after ',', got %s (%q)", p.peek().Type, p.peek().Val)
			}
			continue
		}
		if p.peek().Type == lexer.TokenMinus {
			return nil, fmt.Errorf("sort: expected ',' between sort keys, got '-'")
		}
		if p.peek().Type == lexer.TokenIdent || p.peek().Type == lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("sort: expected ',' between sort keys, got %q", p.peek().Val)
		}
		break
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("sort: expected at least one column")
	}
	return &ast.SortOp{Keys: keys}, nil
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
	nestedName := "grouped"

	if p.peek().Type == lexer.TokenIdent {
		if p.peekAt(1).Type == lexer.TokenEquals {
			// "col = expr" pattern -- no nested name
		} else if (p.peekAt(1).Type == lexer.TokenIdent || p.peekAt(1).Type == lexer.TokenBacktickIdent) &&
			p.peekAt(2).Type == lexer.TokenEquals {
			// "nested_name col = expr" pattern
			nestedName = p.advance().Val
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
		if _, err := p.expect(lexer.TokenEquals); err != nil {
			return nil, fmt.Errorf("rename: expected '=' after column %q: %w", oldTok.Val, err)
		}
		newTok := p.advance()
		if newTok.Type != lexer.TokenIdent && newTok.Type != lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("rename: expected new column name, got %s (%q)", newTok.Type, newTok.Val)
		}
		pairs = append(pairs, ast.RenamePair{Old: oldTok.Val, New: newTok.Val})
		if p.peek().Type != lexer.TokenComma {
			break
		}
		p.advance()
		if p.peek().Type != lexer.TokenIdent && p.peek().Type != lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("rename: expected column name after ',', got %s (%q)", p.peek().Type, p.peek().Val)
		}
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("rename: expected at least one old=new pair")
	}
	return &ast.RenameOp{Pairs: pairs}, nil
}

var joinKinds = map[string]bool{
	"inner": true,
	"left":  true,
	"right": true,
	"full":  true,
}

func (p *Parser) parseJoin() (ast.Op, error) {
	p.advance() // consume "join"

	// ScanSource reads directly from the lexer; any buffered lookahead token
	// has already been consumed from the input and would be silently lost.
	// Op dispatch leaves the buffer empty here, so a non-empty buffer means a
	// parser bug -- fail loudly instead of mis-scanning the filename.
	if len(p.buf) != 0 {
		return nil, fmt.Errorf("join: internal error: lookahead buffer not empty before filename")
	}

	kind := "inner"
	filename, err := p.lexer.ScanSource()
	if err != nil {
		return nil, fmt.Errorf("join: %w", err)
	}
	seenOn := false
	switch filename.Type {
	case lexer.TokenIdent:
		if joinKinds[filename.Val] {
			kindWord := filename.Val
			kind = kindWord
			fileTok, err := p.lexer.ScanSource()
			if err != nil {
				return nil, fmt.Errorf("join: %w", err)
			}
			if fileTok.Type == lexer.TokenIdent && fileTok.Val == "on" {
				filename = lexer.Token{Type: lexer.TokenIdent, Val: kindWord, Pos: filename.Pos}
				kind = "inner"
				seenOn = true
			} else if fileTok.Type == lexer.TokenIdent || fileTok.Type == lexer.TokenStdin {
				filename = fileTok
			} else {
				return nil, fmt.Errorf("join: expected filename after %q", kind)
			}
		}
	case lexer.TokenStdin:
		// keep as filename "-"
	default:
		return nil, fmt.Errorf("join: expected filename at position %d", filename.Pos)
	}

	if filename.Type == lexer.TokenStdin {
		return nil, fmt.Errorf("join: stdin is not supported as join source")
	}
	if filename.Val == "" {
		return nil, fmt.Errorf("join: expected filename")
	}

	load, err := p.parseOptionalWithClause()
	if err != nil {
		return nil, fmt.Errorf("join: %w", err)
	}
	if err := ast.ValidateLoadOptionsForFilename(filename.Val, load); err != nil {
		return nil, fmt.Errorf("join: %w", err)
	}

	if !seenOn {
		onTok := p.advance()
		if onTok.Type != lexer.TokenIdent || onTok.Val != "on" {
			if onTok.Type == lexer.TokenWith {
				return nil, fmt.Errorf("join: with clause must appear before on")
			}
			return nil, fmt.Errorf("join: expected 'on', got %q", onTok.Val)
		}
	}

	keys, err := p.parseJoinKeys()
	if err != nil {
		return nil, fmt.Errorf("join: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("join: expected at least one join key")
	}

	return &ast.JoinOp{Kind: kind, Filename: filename.Val, Keys: keys, Load: load}, nil
}

func (p *Parser) parseJoinKeys() ([]ast.JoinKey, error) {
	var keys []ast.JoinKey
	for {
		left, err := p.parseOneColumnPath()
		if err != nil {
			return nil, err
		}
		right := append([]string(nil), left...)
		if p.peek().Type == lexer.TokenEq {
			p.advance()
			right, err = p.parseOneColumnPath()
			if err != nil {
				return nil, err
			}
		}
		keys = append(keys, ast.JoinKey{Left: left, Right: right})
		if p.peek().Type != lexer.TokenAnd {
			break
		}
		p.advance()
	}
	return keys, nil
}

func (p *Parser) parseOneColumnPath() ([]string, error) {
	t := p.peek()
	if t.Type != lexer.TokenIdent && t.Type != lexer.TokenBacktickIdent {
		return nil, fmt.Errorf("expected column name, got %s (%q)", t.Type, t.Val)
	}
	tok := p.advance()
	path := []string{tok.Val}
	for p.peek().Type == lexer.TokenDot {
		p.advance()
		seg := p.advance()
		if seg.Type != lexer.TokenIdent && seg.Type != lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("expected field name after '.', got %s (%q)", seg.Type, seg.Val)
		}
		path = append(path, seg.Val)
	}
	return path, nil
}

// parseColumnListOpts reads comma-separated dot-path column names.
// When stopAtAs is true, parsing stops before an "as" keyword (for group).
func (p *Parser) parseColumnListOpts(stopAtAs bool) ([][]string, error) {
	var cols [][]string
	for {
		if stopAtAs && p.peek().Type == lexer.TokenAs {
			break
		}
		if p.peek().Type != lexer.TokenIdent && p.peek().Type != lexer.TokenBacktickIdent {
			break
		}
		path, err := p.parseOneColumnPath()
		if err != nil {
			return nil, err
		}
		cols = append(cols, path)

		if stopAtAs && p.peek().Type == lexer.TokenAs {
			break
		}
		if p.peek().Type == lexer.TokenComma {
			p.advance()
			if stopAtAs && p.peek().Type == lexer.TokenAs {
				return nil, fmt.Errorf("expected column name after ',', got %s (%q)", p.peek().Type, p.peek().Val)
			}
			if p.peek().Type != lexer.TokenIdent && p.peek().Type != lexer.TokenBacktickIdent {
				return nil, fmt.Errorf("expected column name after ',', got %s (%q)", p.peek().Type, p.peek().Val)
			}
			continue
		}
		if p.peek().Type == lexer.TokenIdent || p.peek().Type == lexer.TokenBacktickIdent {
			return nil, fmt.Errorf("expected ',' between columns, got %q", p.peek().Val)
		}
		break
	}
	return cols, nil
}

// parseColumnList reads comma-separated dot-path column names.
func (p *Parser) parseColumnList() ([][]string, error) {
	return p.parseColumnListOpts(false)
}

// parseColumnListUntilAs reads comma-separated columns but stops at "as".
func (p *Parser) parseColumnListUntilAs() ([][]string, error) {
	return p.parseColumnListOpts(true)
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
	for _, path := range cols {
		if len(path) > 1 {
			return nil, fmt.Errorf("remove: dot paths not supported, got %q", strings.Join(path, "."))
		}
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
	precOr     = 1
	precAnd    = 2
	precIsNull = 3
	precComp   = 3
	precAdd    = 4
	precMul    = 5
	precUnary  = 6
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
		if minPrec <= precIsNull {
			var applied bool
			left, applied, err = p.parsePostfixIsNull(left)
			if err != nil {
				return nil, err
			}
			if applied {
				continue
			}
		}

		op, prec, ok := p.peekBinaryOp()
		if !ok || prec < minPrec {
			break
		}
		p.advanceBinaryOp(op) // consume the operator token(s)

		right, err := p.parseExprPrec(prec + 1) // left-associative
		if err != nil {
			return nil, err
		}
		if (op == "==" || op == "!=") && (isNullLiteral(left) || isNullLiteral(right)) {
			return nil, nullComparisonError(op, left, right)
		}
		left = &ast.BinaryExpr{Op: op, Left: left, Right: right}
	}

	if minPrec <= precIsNull {
		left, _, err = p.parsePostfixIsNull(left)
		if err != nil {
			return nil, err
		}
	}

	return left, nil
}

func (p *Parser) parsePostfixIsNull(left ast.Expr) (ast.Expr, bool, error) {
	if p.peek().Type != lexer.TokenIs {
		return left, false, nil
	}
	p.advance() // consume "is"
	negated := false
	if p.peek().Type == lexer.TokenNot {
		p.advance() // consume "not"
		negated = true
	}
	if _, err := p.expect(lexer.TokenNull); err != nil {
		return nil, false, fmt.Errorf("expected 'null' after 'is%s'", func() string {
			if negated {
				return " not"
			}
			return ""
		}())
	}
	return &ast.IsNullExpr{Operand: left, Negated: negated}, true, nil
}

func isNullLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.LiteralExpr)
	return ok && lit.Kind == "null"
}

// nullComparisonError rejects "expr == null" / "expr != null" with a hint
// pointing to the documented "is null" / "is not null" syntax.
func nullComparisonError(op string, left, right ast.Expr) error {
	isForm := "is null"
	if op == "!=" {
		isForm = "is not null"
	}
	other := left
	if isNullLiteral(other) {
		other = right
	}
	if col, ok := other.(*ast.ColumnExpr); ok {
		name := strings.Join(col.Path, ".")
		return fmt.Errorf("use %q instead of %q for null checks", name+" "+isForm, name+" "+op+" null")
	}
	return fmt.Errorf("use \"is null\" / \"is not null\" for null checks, not %s", op)
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
	primary, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	if isLiteralExpr(primary) {
		return primary, nil
	}
	left, _, err := p.parsePostfixIsNull(primary)
	if err != nil {
		return nil, err
	}
	return left, nil
}

func isLiteralExpr(e ast.Expr) bool {
	_, ok := e.(*ast.LiteralExpr)
	return ok
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
		path := []string{tok.Val}
		for p.peek().Type == lexer.TokenDot {
			p.advance() // consume .
			seg := p.advance()
			if seg.Type != lexer.TokenIdent && seg.Type != lexer.TokenBacktickIdent {
				return nil, fmt.Errorf("expected field name after '.', got %s (%q)", seg.Type, seg.Val)
			}
			path = append(path, seg.Val)
		}
		return &ast.ColumnExpr{Path: path}, nil

	case lexer.TokenIdent:
		p.advance()
		// Check if it's a function call
		if p.peek().Type == lexer.TokenLParen {
			return p.parseFuncCall(tok.Val)
		}
		path := []string{tok.Val}
		for p.peek().Type == lexer.TokenDot {
			p.advance() // consume .
			seg := p.advance()
			if seg.Type != lexer.TokenIdent && seg.Type != lexer.TokenBacktickIdent {
				return nil, fmt.Errorf("expected field name after '.', got %s (%q)", seg.Type, seg.Val)
			}
			path = append(path, seg.Val)
		}
		return &ast.ColumnExpr{Path: path}, nil

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
