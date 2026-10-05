package ast

import "tl/internal/tl/token"

type Type int

const (
	TInt Type = iota
	TBool
	tfuncBase
)

// FuncSig is the parameter/result shape of a function type.
type FuncSig struct {
	Params []Type
	Result Type
}

var (
	funcSigs   []*FuncSig
	funcIntern = map[string]Type{}
)

// FuncOf returns the (interned) function type fun(params...) -> result.
// Interning keeps Type comparable with ==, which the checker relies on.
func FuncOf(params []Type, result Type) Type {
	key := "("
	for i, p := range params {
		if i > 0 {
			key += ","
		}
		key += p.String()
	}
	key += ")->" + result.String()
	if t, ok := funcIntern[key]; ok {
		return t
	}
	funcSigs = append(funcSigs, &FuncSig{Params: params, Result: result})
	t := tfuncBase + Type(len(funcSigs)-1)
	funcIntern[key] = t
	return t
}

func (t Type) IsFunc() bool { return t >= tfuncBase }

// Sig returns the signature of a function type; panics otherwise.
func (t Type) Sig() *FuncSig { return funcSigs[t-tfuncBase] }

func (t Type) String() string {
	switch t {
	case TInt:
		return "int"
	case TBool:
		return "bool"
	default:
		if t.IsFunc() {
			sig := t.Sig()
			s := "fun("
			for i, p := range sig.Params {
				if i > 0 {
					s += ", "
				}
				s += p.String()
			}
			return s + ") -> " + sig.Result.String()
		}
		return "<unknown>"
	}
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

func (f *FuncDecl) Pos() token.Pos { return f.Posn }

type Block struct {
	Stmts []Stmt
	Posn  token.Pos
}

func (b *Block) Pos() token.Pos { return b.Posn }

type Stmt interface {
	Node
	stmtNode()
}

type LetStmt struct {
	Name string
	Type Type // declared type; also resolved expression type after checking
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
	// ViaCap is set by the checker when the target is a captured variable
	// of an enclosing function; Slot is then a capture index.
	ViaCap bool
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
	Slot  int
	TType Type
	// ViaCap marks a reference to a captured variable (Slot is a capture
	// index). IsFunc marks a reference to a top-level function used as a
	// value (FuncIndex is its program-level function index).
	ViaCap    bool
	IsFunc    bool
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
	Name string
	Args []Expr
	Posn token.Pos
	// Result is populated by the checker.
	Result Type
	// ValueCall is set by the checker when Name denotes a variable holding
	// a function value rather than a top-level or built-in function.
	// Slot/ViaCap then describe how to load that variable.
	ValueCall bool
	Slot      int
	ViaCap    bool
	Builtin   bool
	FuncIndex int
}

func (e *CallExpr) Pos() token.Pos { return e.Posn }
func (*CallExpr) exprNode()        {}

// CallValueExpr calls the function value produced by an arbitrary
// expression, e.g. makeAdder(1)(2).
type CallValueExpr struct {
	Callee Expr
	Args   []Expr
	Posn   token.Pos
	// Result is populated by the checker.
	Result Type
}

func (e *CallValueExpr) Pos() token.Pos { return e.Posn }
func (*CallValueExpr) exprNode()        {}

// Capture describes one variable captured by a FuncExpr. FromLocal means
// the cell lives in the enclosing function's local slot Index; otherwise
// it is the enclosing function's own capture Index.
type Capture struct {
	FromLocal bool
	Index     int
}

// FuncExpr is an anonymous function expression (a closure). The checker
// resolves its free variables into Captures and marks every captured
// local of the enclosing function as boxed (cell-backed).
type FuncExpr struct {
	Params []*Param
	Result Type
	Body   *Block
	Posn   token.Pos
	TType  Type
	// Resolver fields, populated by the checker.
	Index     int          // index into checker.Result.Lambdas / IR functions
	NumLocals int          // local slots of the generated function
	Caps      []Capture    // cells to load from the enclosing frame
	Boxed     map[int]bool // local slots stored as cells
}

func (e *FuncExpr) Pos() token.Pos { return e.Posn }
func (*FuncExpr) exprNode()        {}
