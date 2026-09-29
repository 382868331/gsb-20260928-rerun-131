package tl

const IRVersion = 1

type IRProgram struct {
	Version   int          `json:"version"`
	Functions []IRFunction `json:"functions"`
}

type IRFunction struct {
	Name      string        `json:"name"`
	Params    []Type        `json:"params"`
	Result    Type          `json:"result"`
	LocalSize int           `json:"local_size"`
	Code      []Instruction `json:"code"`
}

type Instruction struct {
	Op  string   `json:"op"`
	Arg int64    `json:"arg,omitempty"`
	Pos Position `json:"pos"`
}

const (
	OpConstInt     = "CONST_INT"
	OpConstBool    = "CONST_BOOL"
	OpLoadLocal    = "LOAD_LOCAL"
	OpStoreLocal   = "STORE_LOCAL"
	OpPop          = "POP"
	OpAdd          = "ADD"
	OpSubtract     = "SUB"
	OpMultiply     = "MUL"
	OpDivide       = "DIV"
	OpNegate       = "NEG"
	OpNot          = "NOT"
	OpEqual        = "EQ"
	OpNotEqual     = "NE"
	OpLess         = "LT"
	OpLessEqual    = "LE"
	OpGreater      = "GT"
	OpGreaterEqual = "GE"
	OpAnd          = "AND"
	OpOr           = "OR"
	OpJump         = "JUMP"
	OpJumpIfFalse  = "JUMP_IF_FALSE"
	OpCall         = "CALL"
	OpReturn       = "RETURN"
	OpPrint        = "PRINT"
)

var validOpcodes = map[string]bool{
	OpConstInt: true, OpConstBool: true, OpLoadLocal: true, OpStoreLocal: true,
	OpPop: true, OpAdd: true, OpSubtract: true, OpMultiply: true, OpDivide: true,
	OpNegate: true, OpNot: true, OpEqual: true, OpNotEqual: true, OpLess: true,
	OpLessEqual: true, OpGreater: true, OpGreaterEqual: true, OpAnd: true, OpOr: true,
	OpJump: true, OpJumpIfFalse: true, OpCall: true, OpReturn: true, OpPrint: true,
}
