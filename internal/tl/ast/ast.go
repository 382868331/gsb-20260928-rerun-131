package ast

import "tl/internal/tl/token"

type Type interface {
	String() string
	typeNode()
}

type Primitive int

const (
	TInt Primitive = iota
	TBool
)

func (Primitive) typeNode() {}

func (t Primitive) String() string {
	switch t {
	case TInt:
		return "int"
	case TBool:
		return "bool"
	default:
		return "<unknown>"
	}
}

type FuncType struct {
	Params []Type
	Result Type
	Posn   token.Pos
}

func (*FuncType) typeNode() {}
func (t *FuncType) String() string {
	s := "fun("
	for i, p := range t.Params {
		if i > 0 {
			s += ", "
		}
		s += p.String()
	}
	s += ") -> " + t.Result.String()
	return s
}

func SameType(a, b Type) bool {
	af, aok := a.(*FuncType)
	bf, bok := b.(*FuncType)
	if aok || bok {
		if !aok || !bok || len(af.Params) != len(bf.Params) {
			return false
		}
		for i := range af.Params {
			if !SameType(af.Params[i], bf.Params[i]) {
				return false
			}
		}
		return SameType(af.Result, bf.Result)
	}
	return a == b
}

type Node interface{ Pos() token.Pos }

type Program struct {
	Funcs []*FuncDecl
}

type Param struct {
	Name string
	Type Type
	Posn token.Pos
}

func (p *Param) Pos() token.Pos { return p.Posn }

type FuncDecl struct {
	Name   string
	Params []*Param
	Result Type
	Body   *Block
	Posn   token.Pos
}

type FuncLit struct {
	Name     string // optional name used for recursive function expressions
	Params   []*Param
	Result   Type
	Body     *Block
	Posn     token.Pos
	TType    Type
	Index    int
	SelfSlot int
}

func (e *FuncLit) Pos() token.Pos { return e.Posn }
func (*FuncLit) exprNode()        {}

func (f *FuncDecl) Pos() token.Pos { return f.Posn }

type Block struct {
	Stmts     []Stmt
	Posn      token.Pos
	SlotBase  int
	ScopeSize int
}

func (b *Block) Pos() token.Pos { return b.Posn }

type Stmt interface {
	Node
	stmtNode()
}

type LetStmt struct {
	Name string
	Type Type
	Init Expr
	Posn token.Pos
	Slot int
}

func (s *LetStmt) Pos() token.Pos { return s.Posn }
func (*LetStmt) stmtNode()        {}

type AssignStmt struct {
	Name  string
	Value Expr
	Posn  token.Pos
	Slot  int
}

func (s *AssignStmt) Pos() token.Pos { return s.Posn }
func (*AssignStmt) stmtNode()        {}

type ExprStmt struct {
	X    Expr
	Posn token.Pos
}

func (s *ExprStmt) Pos() token.Pos { return s.Posn }
func (*ExprStmt) stmtNode()        {}

type IfStmt struct {
	Cond Expr
	Then *Block
	Else Stmt // *Block or *IfStmt or nil
	Posn token.Pos
}

func (s *IfStmt) Pos() token.Pos { return s.Posn }
func (*IfStmt) stmtNode()        {}

type WhileStmt struct {
	Cond Expr
	Body *Block
	Posn token.Pos
}

func (s *WhileStmt) Pos() token.Pos { return s.Posn }
func (*WhileStmt) stmtNode()        {}

type ReturnStmt struct {
	Value Expr
	Posn  token.Pos
}

func (s *ReturnStmt) Pos() token.Pos { return s.Posn }
func (*ReturnStmt) stmtNode()        {}

type BlockStmt struct {
	Block *Block
}

func (s *BlockStmt) Pos() token.Pos { return s.Block.Posn }
func (*BlockStmt) stmtNode()        {}

type Expr interface {
	Node
	exprNode()
}

type IntLit struct {
	Value int64
	Posn  token.Pos
	TType Type
}

func (e *IntLit) Pos() token.Pos { return e.Posn }
func (*IntLit) exprNode()        {}

type BoolLit struct {
	Value bool
	Posn  token.Pos
	TType Type
}

func (e *BoolLit) Pos() token.Pos { return e.Posn }
func (*BoolLit) exprNode()        {}

type Ident struct {
	Name string
	Posn token.Pos
	// Resolver fields, populated by the checker. They deliberately do not
	// change the parser/AST boundary: a parsed program simply has zeros.
	Slot      int
	TType     Type
	FuncIndex int
}

func (e *Ident) Pos() token.Pos { return e.Posn }
func (*Ident) exprNode()        {}

type UnaryExpr struct {
	Op    token.Kind
	X     Expr
	Posn  token.Pos
	TType Type // checker result
}

func (e *UnaryExpr) Pos() token.Pos { return e.Posn }
func (*UnaryExpr) exprNode()        {}

type BinaryExpr struct {
	Op    token.Kind
	L     Expr
	R     Expr
	Posn  token.Pos
	TType Type // checker result
}

func (e *BinaryExpr) Pos() token.Pos { return e.Posn }
func (*BinaryExpr) exprNode()        {}

type CallExpr struct {
	Name    string
	Callee  Expr
	Args    []Expr
	Posn    token.Pos
	Result  Type
	Builtin string
	Dynamic bool
}

func (e *CallExpr) Pos() token.Pos { return e.Posn }
func (*CallExpr) exprNode()        {}
