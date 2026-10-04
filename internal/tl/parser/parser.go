package parser

import (
	"tl/internal/tl/ast"
	"tl/internal/tl/lexer"
	"tl/internal/tl/tlerr"
	"tl/internal/tl/token"
)

type Parser struct {
	file string
	toks []token.Token
	pos  int
}

func Parse(file string, src []byte) (*ast.Program, error) {
	toks, err := lexer.Tokenize(file, src)
	if err != nil {
		return nil, err
	}
	p := &Parser{file: file, toks: toks}
	return p.parseProgram()
}

func (p *Parser) cur() token.Token     { return p.toks[p.pos] }
func (p *Parser) peek() token.Kind     { return p.cur().Kind }
func (p *Parser) at(k token.Kind) bool { return p.peek() == k }

func (p *Parser) advance() token.Token {
	t := p.cur()
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) want(k token.Kind) (token.Token, error) {
	if p.peek() != k {
		return token.Token{}, p.errf(p.cur().Pos, "expected %s, found %s", k, p.cur().Kind)
	}
	return p.advance(), nil
}

func (p *Parser) errf(pos token.Pos, format string, args ...any) error {
	return tlerr.At(p.file, pos, format, args...)
}

func (p *Parser) parseProgram() (*ast.Program, error) {
	prog := &ast.Program{}
	for !p.at(token.EOF) {
		if !p.at(token.FUN) {
			return nil, p.errf(p.cur().Pos, "expected top-level function declaration, found %s", p.cur().Kind)
		}
		fn, err := p.parseFunc()
		if err != nil {
			return nil, err
		}
		prog.Funcs = append(prog.Funcs, fn)
	}
	return prog, nil
}

func (p *Parser) parseFunc() (*ast.FuncDecl, error) {
	start, _ := p.want(token.FUN)
	nameTok, err := p.want(token.IDENT)
	if err != nil {
		return nil, err
	}
	params, result, err := p.parseSignature()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.FuncDecl{Name: nameTok.Lit, Params: params, Result: result, Body: body, Posn: start.Pos}, nil
}

func (p *Parser) parseSignature() ([]*ast.Param, ast.Type, error) {
	if _, err := p.want(token.LPAREN); err != nil {
		return nil, nil, err
	}
	var params []*ast.Param
	if !p.at(token.RPAREN) {
		for {
			name, err := p.want(token.IDENT)
			if err != nil {
				return nil, nil, err
			}
			if _, err := p.want(token.COLON); err != nil {
				return nil, nil, err
			}
			ty, err := p.parseType()
			if err != nil {
				return nil, nil, err
			}
			params = append(params, &ast.Param{Name: name.Lit, Type: ty, Posn: name.Pos})
			if !p.at(token.COMMA) {
				break
			}
			p.advance()
		}
	}
	if _, err := p.want(token.RPAREN); err != nil {
		return nil, nil, err
	}
	result := ast.Type(ast.TInt)
	if p.at(token.ARROW) {
		p.advance()
		var err error
		result, err = p.parseType()
		if err != nil {
			return nil, nil, err
		}
	}
	return params, result, nil
}

func (p *Parser) parseFuncLit(start token.Token) (*ast.FuncLit, error) {
	name := ""
	if p.at(token.IDENT) {
		name = p.advance().Lit
	}
	params, result, err := p.parseSignature()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.FuncLit{Name: name, Params: params, Result: result, Body: body, Posn: start.Pos, Index: -1, SelfSlot: -1}, nil
}

func (p *Parser) parseType() (ast.Type, error) {
	tok := p.advance()
	switch tok.Kind {
	case token.INTK:
		return ast.Type(ast.TInt), nil
	case token.BOOLK:
		return ast.Type(ast.TBool), nil
	case token.FUN:
		if _, err := p.want(token.LPAREN); err != nil {
			return nil, err
		}
		var params []ast.Type
		if !p.at(token.RPAREN) {
			for {
				t, err := p.parseType()
				if err != nil {
					return nil, err
				}
				params = append(params, t)
				if !p.at(token.COMMA) {
					break
				}
				p.advance()
			}
		}
		if _, err := p.want(token.RPAREN); err != nil {
			return nil, err
		}
		if _, err := p.want(token.ARROW); err != nil {
			return nil, err
		}
		result, err := p.parseType()
		if err != nil {
			return nil, err
		}
		return &ast.FuncType{Params: params, Result: result, Posn: tok.Pos}, nil
	default:
		return nil, p.errf(tok.Pos, "expected type, found %s", tok.Kind)
	}
}

func (p *Parser) parseBlock() (*ast.Block, error) {
	lb, err := p.want(token.LBRACE)
	if err != nil {
		return nil, err
	}
	b := &ast.Block{Posn: lb.Pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		b.Stmts = append(b.Stmts, s)
	}
	if _, err := p.want(token.RBRACE); err != nil {
		return nil, err
	}
	return b, nil
}

func (p *Parser) parseStmt() (ast.Stmt, error) {
	switch p.peek() {
	case token.LET:
		return p.parseLet()
	case token.IF:
		return p.parseIf()
	case token.WHILE:
		return p.parseWhile()
	case token.RETURN:
		return p.parseReturn()
	case token.LBRACE:
		b, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.BlockStmt{Block: b}, nil
	case token.IDENT:
		if p.toks[p.pos+1].Kind == token.ASSIGN {
			name := p.advance()
			p.advance()
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.terminate(); err != nil {
				return nil, err
			}
			return &ast.AssignStmt{Name: name.Lit, Value: val, Posn: name.Pos}, nil
		}
	}
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.terminate(); err != nil {
		return nil, err
	}
	return &ast.ExprStmt{X: x, Posn: x.Pos()}, nil
}

func (p *Parser) terminate() error {
	if p.at(token.SEMI) {
		p.advance()
		return nil
	}
	if p.at(token.RBRACE) || p.at(token.EOF) {
		return nil
	}
	return p.errf(p.cur().Pos, "expected ';' or end of statement, found %s", p.cur().Kind)
}

func (p *Parser) parseLet() (ast.Stmt, error) {
	let := p.advance()
	name, err := p.want(token.IDENT)
	if err != nil {
		return nil, err
	}
	if _, err := p.want(token.COLON); err != nil {
		return nil, err
	}
	ty, err := p.parseType()
	if err != nil {
		return nil, err
	}
	s := &ast.LetStmt{Name: name.Lit, Type: ty, Posn: let.Pos}
	if p.at(token.ASSIGN) {
		p.advance()
		s.Init, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if err := p.terminate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (p *Parser) parseIf() (ast.Stmt, error) {
	tok := p.advance()
	if _, err := p.want(token.LPAREN); err != nil {
		return nil, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.want(token.RPAREN); err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	s := &ast.IfStmt{Cond: cond, Then: then, Posn: tok.Pos}
	if p.at(token.ELSE) {
		p.advance()
		if p.at(token.IF) {
			s.Else, err = p.parseIf()
			if err != nil {
				return nil, err
			}
		} else {
			b, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			s.Else = &ast.BlockStmt{Block: b}
		}
	}
	return s, nil
}

func (p *Parser) parseWhile() (ast.Stmt, error) {
	tok := p.advance()
	if _, err := p.want(token.LPAREN); err != nil {
		return nil, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.want(token.RPAREN); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.WhileStmt{Cond: cond, Body: body, Posn: tok.Pos}, nil
}

func (p *Parser) parseReturn() (ast.Stmt, error) {
	tok := p.advance()
	val, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.terminate(); err != nil {
		return nil, err
	}
	return &ast.ReturnStmt{Value: val, Posn: tok.Pos}, nil
}

func (p *Parser) parseExpr() (ast.Expr, error) { return p.parseOr() }

func (p *Parser) parseBinary(operands map[token.Kind]bool, next func() (ast.Expr, error)) (ast.Expr, error) {
	l, err := next()
	if err != nil {
		return nil, err
	}
	for operands[p.peek()] {
		op := p.advance()
		r, err := next()
		if err != nil {
			return nil, err
		}
		l = &ast.BinaryExpr{Op: op.Kind, L: l, R: r, Posn: l.Pos()}
	}
	return l, nil
}

func (p *Parser) parseOr() (ast.Expr, error) {
	return p.parseBinary(map[token.Kind]bool{token.OR: true}, p.parseAnd)
}

func (p *Parser) parseAnd() (ast.Expr, error) {
	return p.parseBinary(map[token.Kind]bool{token.AND: true}, p.parseEquality)
}

func (p *Parser) parseEquality() (ast.Expr, error) {
	return p.parseBinary(map[token.Kind]bool{token.EQ: true, token.NE: true}, p.parseRelational)
}

func (p *Parser) parseRelational() (ast.Expr, error) {
	return p.parseBinary(map[token.Kind]bool{token.LT: true, token.GT: true, token.LE: true, token.GE: true}, p.parseAdditive)
}

func (p *Parser) parseAdditive() (ast.Expr, error) {
	return p.parseBinary(map[token.Kind]bool{token.PLUS: true, token.MINUS: true}, p.parseMultiplicative)
}

func (p *Parser) parseMultiplicative() (ast.Expr, error) {
	return p.parseBinary(map[token.Kind]bool{token.STAR: true, token.SLASH: true, token.PERCENT: true}, p.parseUnary)
}

func (p *Parser) parseUnary() (ast.Expr, error) {
	if p.at(token.BANG) || p.at(token.MINUS) {
		op := p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: op.Kind, X: x, Posn: op.Pos}, nil
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() (ast.Expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.at(token.LPAREN) {
		p.advance()
		call := &ast.CallExpr{Callee: e, Posn: e.Pos()}
		if id, ok := e.(*ast.Ident); ok {
			call.Name = id.Name
		}
		if !p.at(token.RPAREN) {
			for {
				arg, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				call.Args = append(call.Args, arg)
				if !p.at(token.COMMA) {
					break
				}
				p.advance()
			}
		}
		if _, err := p.want(token.RPAREN); err != nil {
			return nil, err
		}
		e = call
	}
	return e, nil
}

func (p *Parser) parsePrimary() (ast.Expr, error) {
	tok := p.cur()
	switch tok.Kind {
	case token.INT:
		p.advance()
		return &ast.IntLit{Value: tok.Value, Posn: tok.Pos}, nil
	case token.TRUE, token.FALSE:
		p.advance()
		return &ast.BoolLit{Value: tok.Kind == token.TRUE, Posn: tok.Pos}, nil
	case token.FUN:
		p.advance()
		return p.parseFuncLit(tok)
	case token.LPAREN:
		p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.want(token.RPAREN); err != nil {
			return nil, err
		}
		return x, nil
	case token.IDENT:
		p.advance()
		return &ast.Ident{Name: tok.Lit, Posn: tok.Pos, Slot: -1}, nil
	default:
		return nil, p.errf(tok.Pos, "expected expression, found %s", tok.Kind)
	}
}
