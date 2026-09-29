package token

import "fmt"

type Kind int

const (
	EOF Kind = iota
	ILLEGAL
	IDENT
	INT

	FUN
	LET
	IF
	ELSE
	WHILE
	RETURN
	TRUE
	FALSE
	INTK
	BOOLK

	ASSIGN // =
	EQ
	NE
	LT
	GT
	LE
	GE
	PLUS
	MINUS
	STAR
	SLASH
	PERCENT
	BANG
	AND
	OR

	ARROW // ->
	COLON
	COMMA
	SEMI
	LPAREN
	RPAREN
	LBRACE
	RBRACE
)

type Pos struct {
	Line   int // 1-based
	Column int // 1-based, rune based
	Offset int // 0-based byte offset
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Column) }

type Token struct {
	Kind  Kind
	Lit   string
	Pos   Pos
	Value int64 // valid when Kind == INT
}

var keywords = map[string]Kind{
	"fun":    FUN,
	"let":    LET,
	"if":     IF,
	"else":   ELSE,
	"while":  WHILE,
	"return": RETURN,
	"true":   TRUE,
	"false":  FALSE,
	"int":    INTK,
	"bool":   BOOLK,
}

func Keyword(s string) (Kind, bool) {
	k, ok := keywords[s]
	return k, ok
}

func (k Kind) String() string {
	switch k {
	case EOF:
		return "end of file"
	case IDENT:
		return "identifier"
	case INT:
		return "integer literal"
	case FUN:
		return "'fun'"
	case LET:
		return "'let'"
	case IF:
		return "'if'"
	case ELSE:
		return "'else'"
	case WHILE:
		return "'while'"
	case RETURN:
		return "'return'"
	case TRUE:
		return "'true'"
	case FALSE:
		return "'false'"
	case INTK:
		return "'int'"
	case BOOLK:
		return "'bool'"
	case ASSIGN:
		return "'='"
	case EQ:
		return "'=='"
	case NE:
		return "'!='"
	case LT:
		return "'<'"
	case GT:
		return ">"
	case LE:
		return "'<='"
	case GE:
		return ">="
	case PLUS:
		return "'+'"
	case MINUS:
		return "'-'"
	case STAR:
		return "'*'"
	case SLASH:
		return "'/'"
	case PERCENT:
		return "'%'"
	case BANG:
		return "'!'"
	case AND:
		return "'&&'"
	case OR:
		return "'||'"
	case ARROW:
		return "'->'"
	case COLON:
		return "':'"
	case COMMA:
		return "','"
	case SEMI:
		return "';'"
	case LPAREN:
		return "'('"
	case RPAREN:
		return "')'"
	case LBRACE:
		return "'{'"
	case RBRACE:
		return "'}'"
	default:
		return "illegal token"
	}
}
