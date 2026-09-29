package tl

import (
	"strconv"
)

type parser struct {
	tokens []token
	pos    int
}

func Parse(source string) (*Program, error) {
	tokens, err := Lex(source)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	program := &Program{}
	if p.peek().kind == tkEOF {
		return nil, errorAt(p.peek().pos, "empty program")
	}
	for !p.accept(tkEOF, "") {
		fn, err := p.parseFunction()
		if err != nil {
			return nil, err
		}
		program.Functions = append(program.Functions, fn)
	}
	return program, nil
}

func (p *parser) parseFunction() (*Function, error) {
	start, err := p.expectKeyword("fn")
	if err != nil {
		return nil, err
	}
	nameTok, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectOperator("("); err != nil {
		return nil, err
	}
	var params []Parameter
	if !p.matchOperator(")") {
		for {
			paramTok, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			if _, err := p.expectOperator(":"); err != nil {
				return nil, err
			}
			typ, _, err := p.expectType()
			if err != nil {
				return nil, err
			}
			params = append(params, Parameter{Name: paramTok.text, Pos: paramTok.pos, Type: typ})
			if p.matchOperator(")") {
				break
			}
			if _, err := p.expectOperator(","); err != nil {
				return nil, err
			}
		}
	}
	if _, err := p.expectOperator("->"); err != nil {
		return nil, err
	}
	result, resultPos, err := p.expectType()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	_ = start
	return &Function{Name: nameTok.text, NamePos: nameTok.pos, Params: params, Result: result, ResultPos: resultPos, Body: body}, nil
}

func (p *parser) parseBlock() (*Block, error) {
	pos, err := p.expectOperator("{")
	if err != nil {
		return nil, err
	}
	block := &Block{Pos: pos}
	for !p.matchOperator("}") {
		if p.peek().kind == tkEOF {
			return nil, errorAt(p.peek().pos, "unterminated block")
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		block.Stmt = append(block.Stmt, stmt)
	}
	return block, nil
}

func (p *parser) parseStatement() (Stmt, error) {
	switch {
	case p.matchOperator("{"):
		p.pos--
		return p.parseBlock()
	case p.matchKeyword("let"):
		return p.parseLet()
	case p.matchKeyword("if"):
		return p.parseIf()
	case p.matchKeyword("while"):
		return p.parseWhile()
	case p.matchKeyword("return"):
		pos := p.previous().pos
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectOperator(";"); err != nil {
			return nil, err
		}
		return &ReturnStmt{Pos: pos, Value: expr}, nil
	case p.matchKeyword("print"):
		pos := p.previous().pos
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectOperator(";"); err != nil {
			return nil, err
		}
		return &PrintStmt{Pos: pos, Value: expr}, nil
	default:
		if p.peek().kind == tkIdent && p.peekText(1) == "=" {
			name := p.next()
			p.next()
			value, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if _, err := p.expectOperator(";"); err != nil {
				return nil, err
			}
			return &AssignStmt{NamePos: name.pos, Name: name.text, Value: value}, nil
		}
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectOperator(";"); err != nil {
			return nil, err
		}
		return &ExprStmt{Value: expr}, nil
	}
}

func (p *parser) parseLet() (Stmt, error) {
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	var typ Type
	if p.matchOperator(":") {
		parsed, _, err := p.expectType()
		if err != nil {
			return nil, err
		}
		typ = parsed
	}
	if _, err := p.expectOperator("="); err != nil {
		return nil, err
	}
	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectOperator(";"); err != nil {
		return nil, err
	}
	return &VarStmt{NamePos: name.pos, Name: name.text, VarType: typ, Value: value}, nil
}

func (p *parser) parseIf() (Stmt, error) {
	pos := p.previous().pos
	cond, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	var elseBlock *Block
	if p.matchKeyword("else") {
		if p.matchKeyword("if") {
			inner, err := p.parseIfFromElse(p.previous().pos)
			if err != nil {
				return nil, err
			}
			elseBlock = &Block{Pos: inner.StatementPos(), Stmt: []Stmt{inner}}
		} else {
			elseBlock, err = p.parseBlock()
			if err != nil {
				return nil, err
			}
		}
	}
	return &IfStmt{Pos: pos, Condition: cond, Then: then, Else: elseBlock}, nil
}

func (p *parser) parseIfFromElse(pos Position) (Stmt, error) {
	cond, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	var elseBlock *Block
	if p.matchKeyword("else") {
		if p.matchKeyword("if") {
			inner, err := p.parseIfFromElse(p.previous().pos)
			if err != nil {
				return nil, err
			}
			elseBlock = &Block{Pos: inner.StatementPos(), Stmt: []Stmt{inner}}
		} else {
			elseBlock, err = p.parseBlock()
			if err != nil {
				return nil, err
			}
		}
	}
	return &IfStmt{Pos: pos, Condition: cond, Then: then, Else: elseBlock}, nil
}

func (p *parser) parseWhile() (Stmt, error) {
	pos := p.previous().pos
	cond, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Pos: pos, Condition: cond, Body: body}, nil
}

func (p *parser) parseExpression() (Expr, error) { return p.parseBinary(0) }

func (p *parser) parseBinary(minPrec int) (Expr, error) {
	var left Expr
	var err error
	if prec, prefix := prefixPrec(p.peek().text); prefix && prec >= minPrec {
		op := p.next()
		right, err := p.parseBinary(prec)
		if err != nil {
			return nil, err
		}
		left = &UnaryExpr{Pos: op.pos, Operator: op.text, Value: right}
	} else {
		left, err = p.parsePrimary()
		if err != nil {
			return nil, err
		}
	}
	for {
		prec, ok := binaryPrec(p.peek().text)
		if !ok || prec < minPrec {
			return left, nil
		}
		op := p.next()
		right, err := p.parseBinary(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Pos: op.pos, Left: left, Operator: op.text, Right: right}
	}
}

func (p *parser) parsePrimary() (Expr, error) {
	tok := p.peek()
	switch {
	case tok.kind == tkInt:
		p.next()
		value, err := strconv.ParseInt(tok.text, 10, 64)
		if err != nil {
			return nil, errorAt(tok.pos, "integer literal is too large")
		}
		return &IntegerLit{Pos: tok.pos, Value: value}, nil
	case tok.kind == tkKeyword && (tok.text == "true" || tok.text == "false"):
		p.next()
		return &BoolLit{Pos: tok.pos, Value: tok.text == "true"}, nil
	case tok.kind == tkOperator && tok.text == "(":
		p.next()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectOperator(")"); err != nil {
			return nil, err
		}
		return expr, nil
	case tok.kind == tkIdent && p.peekText(1) == "(":
		p.next()
		p.next()
		var args []Expr
		if !p.matchOperator(")") {
			for {
				arg, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
				if p.matchOperator(")") {
					break
				}
				if _, err := p.expectOperator(","); err != nil {
					return nil, err
				}
			}
		}
		return &CallExpr{Pos: tok.pos, Name: tok.text, Arguments: args}, nil
	case tok.kind == tkIdent:
		p.next()
		return &IdentExpr{Pos: tok.pos, Name: tok.text}, nil
	default:
		return nil, errorAt(tok.pos, "expected expression, got %s", describeToken(tok))
	}
}

func prefixPrec(op string) (int, bool) {
	if op == "-" || op == "!" {
		return 7, true
	}
	return 0, false
}

func binaryPrec(op string) (int, bool) {
	switch op {
	case "||":
		return 1, true
	case "&&":
		return 2, true
	case "==", "!=":
		return 3, true
	case "<", "<=", ">", ">=":
		return 4, true
	case "+", "-":
		return 5, true
	case "*", "/":
		return 6, true
	default:
		return 0, false
	}
}

func (p *parser) peek() token { return p.tokens[p.pos] }
func (p *parser) peekText(offset int) string {
	if p.pos+offset >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos+offset].text
}
func (p *parser) next() token {
	t := p.peek()
	if t.kind != tkEOF {
		p.pos++
	}
	return t
}
func (p *parser) previous() token { return p.tokens[p.pos-1] }

func (p *parser) accept(kind tokenKind, text string) bool {
	t := p.peek()
	if t.kind != kind || (text != "" && t.text != text) {
		return false
	}
	p.next()
	return true
}
func (p *parser) matchKeyword(text string) bool  { return p.accept(tkKeyword, text) }
func (p *parser) matchOperator(text string) bool { return p.accept(tkOperator, text) }

func (p *parser) expectOperator(text string) (Position, error) {
	if p.matchOperator(text) {
		return p.previous().pos, nil
	}
	return Position{}, errorAt(p.peek().pos, "expected %q, got %s", text, describeToken(p.peek()))
}
func (p *parser) expectKeyword(text string) (Position, error) {
	if p.matchKeyword(text) {
		return p.previous().pos, nil
	}
	return Position{}, errorAt(p.peek().pos, "expected keyword %q, got %s", text, describeToken(p.peek()))
}
func (p *parser) expectIdent() (token, error) {
	t := p.peek()
	if t.kind != tkIdent {
		return token{}, errorAt(t.pos, "expected identifier, got %s", describeToken(t))
	}
	return p.next(), nil
}
func (p *parser) expectType() (Type, Position, error) {
	t := p.peek()
	if t.kind != tkKeyword || (t.text != string(IntType) && t.text != string(BoolType)) {
		return "", t.pos, errorAt(t.pos, "expected int or bool, got %s", describeToken(t))
	}
	p.next()
	return Type(t.text), t.pos, nil
}

func describeToken(t token) string {
	if t.kind == tkEOF {
		return "end of file"
	}
	return "\"" + t.text + "\""
}
