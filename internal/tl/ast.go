package tl

type Position struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type Type string

const (
	IntType  Type = "int"
	BoolType Type = "bool"
)

type Program struct {
	Functions []*Function
}

type Function struct {
	Name      string
	NamePos   Position
	Params    []Parameter
	Result    Type
	ResultPos Position
	Body      *Block
}

type Parameter struct {
	Name string
	Pos  Position
	Type Type
}

type Stmt interface {
	StatementPos() Position
}

type Expr interface {
	ExpressionPos() Position
}

type Block struct {
	Pos  Position
	Stmt []Stmt
}

type VarStmt struct {
	NamePos Position
	Name    string
	VarType Type
	Value   Expr
}

type AssignStmt struct {
	NamePos Position
	Name    string
	Value   Expr
}

type IfStmt struct {
	Pos       Position
	Condition Expr
	Then      *Block
	Else      *Block
}

type WhileStmt struct {
	Pos       Position
	Condition Expr
	Body      *Block
}

type ReturnStmt struct {
	Pos   Position
	Value Expr
}

type PrintStmt struct {
	Pos   Position
	Value Expr
}

type ExprStmt struct {
	Value Expr
}

type IntegerLit struct {
	Pos   Position
	Value int64
}

type BoolLit struct {
	Pos   Position
	Value bool
}

type IdentExpr struct {
	Pos  Position
	Name string
}

type UnaryExpr struct {
	Pos      Position
	Operator string
	Value    Expr
}

type BinaryExpr struct {
	Pos      Position
	Left     Expr
	Operator string
	Right    Expr
}

type CallExpr struct {
	Pos       Position
	Name      string
	Arguments []Expr
}

func (s *Block) StatementPos() Position      { return s.Pos }
func (s *VarStmt) StatementPos() Position    { return s.NamePos }
func (s *AssignStmt) StatementPos() Position { return s.NamePos }
func (s *IfStmt) StatementPos() Position     { return s.Pos }
func (s *WhileStmt) StatementPos() Position  { return s.Pos }
func (s *ReturnStmt) StatementPos() Position { return s.Pos }
func (s *PrintStmt) StatementPos() Position  { return s.Pos }
func (s *ExprStmt) StatementPos() Position   { return s.Value.ExpressionPos() }

func (e *IntegerLit) ExpressionPos() Position { return e.Pos }
func (e *BoolLit) ExpressionPos() Position    { return e.Pos }
func (e *IdentExpr) ExpressionPos() Position  { return e.Pos }
func (e *UnaryExpr) ExpressionPos() Position  { return e.Pos }
func (e *BinaryExpr) ExpressionPos() Position { return e.Pos }
func (e *CallExpr) ExpressionPos() Position   { return e.Pos }
