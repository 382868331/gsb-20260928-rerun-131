package lexer

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"tl/internal/tl/tlerr"
	"tl/internal/tl/token"
)

type Lexer struct {
	file string
	src  []byte

	offset int
	line   int
	col    int
}

func New(file string, src []byte) *Lexer {
	return &Lexer{file: file, src: src, line: 1, col: 1}
}

func (l *Lexer) File() string { return l.file }

func (l *Lexer) posAt(offset int) token.Pos {
	line, col := 1, 1
	for i := 0; i < offset; {
		r, size := utf8.DecodeRune(l.src[i:])
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i += size
	}
	return token.Pos{Line: line, Column: col, Offset: offset}
}

func (l *Lexer) Pos() token.Pos {
	return token.Pos{Line: l.line, Column: l.col, Offset: l.offset}
}

func (l *Lexer) peek() byte {
	if l.offset >= len(l.src) {
		return 0
	}
	return l.src[l.offset]
}

func (l *Lexer) peekAt(n int) byte {
	if l.offset+n >= len(l.src) {
		return 0
	}
	return l.src[l.offset+n]
}

func (l *Lexer) advance() byte {
	b := l.src[l.offset]
	l.offset++
	if b == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return b
}

func (l *Lexer) skipSpaceAndComments() {
	for l.offset < len(l.src) {
		c := l.peek()
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.advance()
		case c == '/' && l.peekAt(1) == '/':
			for l.offset < len(l.src) && l.peek() != '\n' {
				l.advance()
			}
		case c == '/' && l.peekAt(1) == '*':
			l.advance()
			l.advance()
			for l.offset < len(l.src) {
				if l.peek() == '*' && l.peekAt(1) == '/' {
					l.advance()
					l.advance()
					break
				}
				l.advance()
			}
		default:
			return
		}
	}
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c < 0x80 && isAlpha(c)) }
func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool { return c == '_' || (c < 0x80 && (isAlpha(c) || isDigit(c))) }

// Tokenize converts an entire source file into tokens.
func Tokenize(file string, src []byte) ([]token.Token, error) {
	l := New(file, src)
	var toks []token.Token
	for {
		l.skipSpaceAndComments()
		if l.offset >= len(l.src) {
			toks = append(toks, token.Token{Kind: token.EOF, Pos: l.Pos()})
			return toks, nil
		}
		startLine, startCol := l.line, l.col
		startOff := l.offset
		pos := token.Pos{Line: startLine, Column: startCol, Offset: startOff}
		c := l.peek()
		switch {
		case isDigit(c):
			var n int64
			for l.offset < len(l.src) && isDigit(l.peek()) {
				d := int64(l.advance() - '0')
				const maxInt64 = 1<<63 - 1
				if n > maxInt64/10 || (n == maxInt64/10 && d > maxInt64%10) {
					return nil, l.illegal(pos, "integer literal overflows int64")
				}
				n = n*10 + d
			}
			toks = append(toks, token.Token{Kind: token.INT, Pos: pos, Value: n,
				Lit: string(src[startOff:l.offset])})
		case isIdentStart(c):
			for l.offset < len(l.src) && isIdentPart(l.peek()) {
				l.advance()
			}
			lit := string(src[startOff:l.offset])
			kind := token.IDENT
			if k, ok := token.Keyword(lit); ok {
				kind = k
			}
			toks = append(toks, token.Token{Kind: kind, Lit: lit, Pos: pos})
		default:
			tok, ok := l.operator(pos)
			if !ok {
				r, _ := utf8.DecodeRune(src[l.offset:])
				if !unicode.IsPrint(r) {
					return nil, l.illegal(pos, "unexpected byte in source")
				}
				return nil, l.illegal(pos, "unexpected character %q", r)
			}
			toks = append(toks, tok)
		}
	}
}

func (l *Lexer) emit(kind token.Kind, pos token.Pos) token.Token {
	return token.Token{Kind: kind, Pos: pos, Lit: string(l.src[pos.Offset:l.offset])}
}

func (l *Lexer) operator(pos token.Pos) (token.Token, bool) {
	c := l.advance()
	one := func(k token.Kind) (token.Token, bool) { return l.emit(k, pos), true }
	switch c {
	case '+':
		return one(token.PLUS)
	case '-':
		if l.peek() == '>' {
			l.advance()
			return l.emit(token.ARROW, pos), true
		}
		return one(token.MINUS)
	case '*':
		return one(token.STAR)
	case '/':
		return one(token.SLASH)
	case '%':
		return one(token.PERCENT)
	case '(':
		return one(token.LPAREN)
	case ')':
		return one(token.RPAREN)
	case '{':
		return one(token.LBRACE)
	case '}':
		return one(token.RBRACE)
	case ',':
		return one(token.COMMA)
	case ';':
		return one(token.SEMI)
	case ':':
		return one(token.COLON)
	case '=':
		if l.peek() == '=' {
			l.advance()
			return l.emit(token.EQ, pos), true
		}
		return one(token.ASSIGN)
	case '!':
		if l.peek() == '=' {
			l.advance()
			return l.emit(token.NE, pos), true
		}
		return one(token.BANG)
	case '<':
		if l.peek() == '=' {
			l.advance()
			return l.emit(token.LE, pos), true
		}
		return one(token.LT)
	case '>':
		if l.peek() == '=' {
			l.advance()
			return l.emit(token.GE, pos), true
		}
		return one(token.GT)
	case '&':
		if l.peek() == '&' {
			l.advance()
			return l.emit(token.AND, pos), true
		}
	case '|':
		if l.peek() == '|' {
			l.advance()
			return l.emit(token.OR, pos), true
		}
	}
	return token.Token{}, false
}

func (l *Lexer) illegal(pos token.Pos, format string, args ...any) error {
	return tlerr.At(l.file, pos, "%s", fmt.Sprintf(format, args...))
}
