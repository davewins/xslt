package xpath

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lexer tokenizes an XPath 2.0 expression.
type Lexer struct {
	src  string
	pos  int
	peek *Token
}

// NewLexer creates a lexer for the given XPath expression.
func NewLexer(src string) *Lexer {
	return &Lexer{src: src}
}

// Peek returns the next token without consuming it.
func (l *Lexer) Peek() Token {
	if l.peek == nil {
		t := l.next()
		l.peek = &t
	}
	return *l.peek
}

// Next consumes and returns the next token.
func (l *Lexer) Next() Token {
	if l.peek != nil {
		t := *l.peek
		l.peek = nil
		return t
	}
	return l.next()
}

// Expect consumes the next token and returns an error if it doesn't match.
func (l *Lexer) Expect(k TokenKind) (Token, error) {
	t := l.Next()
	if t.Kind != k {
		return t, fmt.Errorf("xpath: expected token %d, got %q at pos %d", k, t.Value, t.Pos)
	}
	return t, nil
}

func (l *Lexer) next() Token {
	l.skipWS()
	if l.pos >= len(l.src) {
		return Token{Kind: TokEOF, Pos: l.pos}
	}
	start := l.pos
	ch, sz := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += sz

	switch {
	case ch == '"' || ch == '\'':
		return l.readString(start, ch)

	case ch >= '0' && ch <= '9':
		return l.readNumber(start)

	case ch == '.':
		if l.pos < len(l.src) {
			next, _ := utf8.DecodeRuneInString(l.src[l.pos:])
			if next == '.' {
				l.pos++
				return Token{Kind: TokDDot, Value: "..", Pos: start}
			}
			if next >= '0' && next <= '9' {
				return l.readNumber(start)
			}
		}
		return Token{Kind: TokDot, Value: ".", Pos: start}

	case ch == '/':
		if l.pos < len(l.src) && l.src[l.pos] == '/' {
			l.pos++
			return Token{Kind: TokDSlash, Value: "//", Pos: start}
		}
		return Token{Kind: TokSlash, Value: "/", Pos: start}

	case ch == ':':
		if l.pos < len(l.src) && l.src[l.pos] == ':' {
			l.pos++
			return Token{Kind: TokDColon, Value: "::", Pos: start}
		}
		return Token{Kind: TokColon, Value: ":", Pos: start}

	case ch == '|':
		return Token{Kind: TokPipe, Value: "|", Pos: start}
	case ch == '+':
		return Token{Kind: TokPlus, Value: "+", Pos: start}
	case ch == '-':
		return Token{Kind: TokMinus, Value: "-", Pos: start}
	case ch == '*':
		return Token{Kind: TokStar, Value: "*", Pos: start}
	case ch == ',':
		return Token{Kind: TokComma, Value: ",", Pos: start}
	case ch == '@':
		return Token{Kind: TokAt, Value: "@", Pos: start}
	case ch == '$':
		return Token{Kind: TokDollar, Value: "$", Pos: start}
	case ch == '(':
		return Token{Kind: TokLParen, Value: "(", Pos: start}
	case ch == ')':
		return Token{Kind: TokRParen, Value: ")", Pos: start}
	case ch == '[':
		return Token{Kind: TokLBrack, Value: "[", Pos: start}
	case ch == ']':
		return Token{Kind: TokRBrack, Value: "]", Pos: start}
	case ch == '{':
		return Token{Kind: TokLBrace, Value: "{", Pos: start}
	case ch == '}':
		return Token{Kind: TokRBrace, Value: "}", Pos: start}
	case ch == '?':
		return Token{Kind: TokQuestion, Value: "?", Pos: start}

	case ch == '=':
		return Token{Kind: TokEq, Value: "=", Pos: start}

	case ch == '!':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return Token{Kind: TokNE, Value: "!=", Pos: start}
		}
		return Token{Kind: TokEOF, Value: "!", Pos: start}

	case ch == '<':
		if l.pos < len(l.src) {
			if l.src[l.pos] == '=' {
				l.pos++
				return Token{Kind: TokLE, Value: "<=", Pos: start}
			}
			if l.src[l.pos] == '<' {
				l.pos++
				return Token{Kind: TokDLT, Value: "<<", Pos: start}
			}
		}
		return Token{Kind: TokLT, Value: "<", Pos: start}

	case ch == '>':
		if l.pos < len(l.src) {
			if l.src[l.pos] == '=' {
				l.pos++
				return Token{Kind: TokGE, Value: ">=", Pos: start}
			}
			if l.src[l.pos] == '>' {
				l.pos++
				return Token{Kind: TokDGT, Value: ">>", Pos: start}
			}
		}
		return Token{Kind: TokGT, Value: ">", Pos: start}

	case isNameStartChar(ch):
		return l.readName(start)

	default:
		return Token{Kind: TokEOF, Value: string(ch), Pos: start}
	}
}

func (l *Lexer) readString(start int, quote rune) Token {
	var sb strings.Builder
	for l.pos < len(l.src) {
		ch, sz := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += sz
		if ch == quote {
			break
		}
		sb.WriteRune(ch)
	}
	return Token{Kind: TokString, Value: sb.String(), Pos: start}
}

func (l *Lexer) readNumber(start int) Token {
	// Back up one char since we already consumed the first digit (or '.')
	l.pos = start
	isFloat := false
	hasE := false

	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch >= '0' && ch <= '9' {
			l.pos++
		} else if ch == '.' && !isFloat {
			isFloat = true
			l.pos++
		} else if (ch == 'e' || ch == 'E') && !hasE {
			hasE = true
			isFloat = true
			l.pos++
			if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
				l.pos++
			}
		} else {
			break
		}
	}
	val := l.src[start:l.pos]
	kind := TokInteger
	if isFloat {
		if hasE {
			kind = TokDouble
		} else {
			kind = TokDecimal
		}
	}
	return Token{Kind: kind, Value: val, Pos: start}
}

func (l *Lexer) readName(start int) Token {
	for l.pos < len(l.src) {
		ch, sz := utf8.DecodeRuneInString(l.src[l.pos:])
		if !isNameChar(ch) {
			break
		}
		l.pos += sz
	}
	name := l.src[start:l.pos]
	if k, ok := keywords[name]; ok {
		return Token{Kind: k, Value: name, Pos: start}
	}
	return Token{Kind: TokName, Value: name, Pos: start}
}

func (l *Lexer) skipWS() {
	for l.pos < len(l.src) {
		ch, sz := utf8.DecodeRuneInString(l.src[l.pos:])
		if !unicode.IsSpace(ch) {
			break
		}
		l.pos += sz
	}
}

func isNameStartChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isNameChar(r rune) bool {
	return r == '_' || r == '-' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
