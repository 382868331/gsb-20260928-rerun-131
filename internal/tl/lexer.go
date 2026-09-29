package tl

import "strings"

type tokenKind int

const (
	tkEOF tokenKind = iota
	tkIdent
	tkInt
	tkKeyword
	tkOperator
)

type token struct {
	kind tokenKind
	text string
	pos  Position
}

type lexer struct {
	src  string
	i    int
	line int
	col  int
}

var keywords = map[string]bool{
	"fn": true, "let": true, "if": true, "else": true, "while": true,
	"return": true, "true": true, "false": true, "int": true, "bool": true, "print": true,
}

func Lex(source string) ([]token, error) {
	l := &lexer{src: source, line: 1, col: 1}
	var tokens []token
	for {
		l.skipSpace()
		if l.i >= len(l.src) {
			tokens = append(tokens, token{kind: tkEOF, pos: l.position()})
			return tokens, nil
		}
		start := l.position()
		ch := l.src[l.i]
		switch {
		case isLetter(ch):
			text := l.readIdentifier()
			kind := tkIdent
			if keywords[text] {
				kind = tkKeyword
			}
			tokens = append(tokens, token{kind: kind, text: text, pos: start})
		case isDigit(ch):
			text := l.readNumber()
			tokens = append(tokens, token{kind: tkInt, text: text, pos: start})
		default:
			op, ok := l.readOperator()
			if !ok {
				return nil, errorAt(start, "unexpected character %q", string(ch))
			}
			tokens = append(tokens, token{kind: tkOperator, text: op, pos: start})
		}
	}
}

func (l *lexer) position() Position { return Position{Line: l.line, Col: l.col} }

func (l *lexer) advance() byte {
	ch := l.src[l.i]
	l.i++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *lexer) skipSpace() {
	for l.i < len(l.src) {
		ch := l.src[l.i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			l.advance()
			continue
		}
		if ch == '/' && l.i+1 < len(l.src) && l.src[l.i+1] == '/' {
			for l.i < len(l.src) && l.src[l.i] != '\n' {
				l.advance()
			}
			continue
		}
		if ch == '/' && l.i+1 < len(l.src) && l.src[l.i+1] == '*' {
			start := l.position()
			l.advance()
			l.advance()
			for l.i+1 < len(l.src) && !(l.src[l.i] == '*' && l.src[l.i+1] == '/') {
				l.advance()
			}
			if l.i+1 >= len(l.src) {
				return
			}
			l.advance()
			l.advance()
			_ = start
			continue
		}
		return
	}
}

func (l *lexer) readIdentifier() string {
	start := l.i
	for l.i < len(l.src) && (isLetter(l.src[l.i]) || isDigit(l.src[l.i])) {
		l.advance()
	}
	return l.src[start:l.i]
}

func (l *lexer) readNumber() string {
	start := l.i
	for l.i < len(l.src) && isDigit(l.src[l.i]) {
		l.advance()
	}
	return l.src[start:l.i]
}

func (l *lexer) readOperator() (string, bool) {
	candidates := []string{"->", "==", "!=", "<=", ">=", "&&", "||"}
	for _, op := range candidates {
		if strings.HasPrefix(l.src[l.i:], op) {
			for range op {
				l.advance()
			}
			return op, true
		}
	}
	if strings.ContainsRune("+-*/(){};,:!=<>!", rune(l.src[l.i])) {
		op := string(l.advance())
		return op, true
	}
	return "", false
}

func isLetter(ch byte) bool { return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') }
func isDigit(ch byte) bool  { return ch >= '0' && ch <= '9' }
