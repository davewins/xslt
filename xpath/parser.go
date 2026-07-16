package xpath

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse parses an XPath 2.0 expression string.
func Parse(expr string) (Expr, error) {
	p := &parser{lex: NewLexer(expr), src: expr}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.lex.Peek().Kind != TokEOF {
		t := p.lex.Peek()
		return nil, fmt.Errorf("xpath: unexpected token %q at pos %d", t.Value, t.Pos)
	}
	return e, nil
}

// MustParse parses or panics — useful in init-time constants.
func MustParse(expr string) Expr {
	e, err := Parse(expr)
	if err != nil {
		panic(err)
	}
	return e
}

// MaxExprDepth bounds the nesting depth of a parsed XPath expression, guarding
// the recursive-descent parser against stack exhaustion from a hostile
// stylesheet (e.g. thousands of nested parentheses). 0 = no limit.
var MaxExprDepth = 1000

type parser struct {
	lex   *Lexer
	src   string
	depth int
}

// parseExpr ::= ExprSingle (',' ExprSingle)*
func (p *parser) parseExpr() (Expr, error) {
	first, err := p.parseExprSingle()
	if err != nil {
		return nil, err
	}
	if p.lex.Peek().Kind != TokComma {
		return first, nil
	}
	seq := &SequenceExpr{Items: []Expr{first}}
	for p.lex.Peek().Kind == TokComma {
		p.lex.Next()
		item, err := p.parseExprSingle()
		if err != nil {
			return nil, err
		}
		seq.Items = append(seq.Items, item)
	}
	return seq, nil
}

// parseExprSingle ::= ForExpr | QuantifiedExpr | IfExpr | OrExpr
func (p *parser) parseExprSingle() (Expr, error) {
	if MaxExprDepth > 0 {
		p.depth++
		if p.depth > MaxExprDepth {
			return nil, fmt.Errorf("xpath: expression nesting exceeds limit of %d", MaxExprDepth)
		}
		defer func() { p.depth-- }()
	}
	switch p.lex.Peek().Kind {
	case TokFor:
		return p.parseForExpr()
	case TokSome, TokEvery:
		return p.parseQuantifiedExpr()
	case TokIf:
		return p.parseIfExpr()
	default:
		return p.parseOrExpr()
	}
}

func (p *parser) parseForExpr() (Expr, error) {
	p.lex.Next() // consume 'for'
	if _, err := p.lex.Expect(TokDollar); err != nil {
		return nil, err
	}
	vt, err := p.lex.Expect(TokName)
	if err != nil {
		return nil, err
	}
	if _, err := p.lex.Expect(TokIn); err != nil {
		return nil, err
	}
	in, err := p.parseExprSingle()
	if err != nil {
		return nil, err
	}
	if _, err := p.lex.Expect(TokReturn); err != nil {
		return nil, err
	}
	body, err := p.parseExprSingle()
	if err != nil {
		return nil, err
	}
	return &ForExpr{Var: vt.Value, In: in, Body: body}, nil
}

func (p *parser) parseQuantifiedExpr() (Expr, error) {
	kind := p.lex.Next().Value // "some" or "every"
	if _, err := p.lex.Expect(TokDollar); err != nil {
		return nil, err
	}
	vt, err := p.lex.Expect(TokName)
	if err != nil {
		return nil, err
	}
	if _, err := p.lex.Expect(TokIn); err != nil {
		return nil, err
	}
	in, err := p.parseExprSingle()
	if err != nil {
		return nil, err
	}
	if _, err := p.lex.Expect(TokSatisfies); err != nil {
		return nil, err
	}
	body, err := p.parseExprSingle()
	if err != nil {
		return nil, err
	}
	return &QuantifiedExpr{Kind: kind, Var: vt.Value, In: in, Body: body}, nil
}

func (p *parser) parseIfExpr() (Expr, error) {
	p.lex.Next() // 'if'
	if _, err := p.lex.Expect(TokLParen); err != nil {
		return nil, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.lex.Expect(TokRParen); err != nil {
		return nil, err
	}
	if _, err := p.lex.Expect(TokThen); err != nil {
		return nil, err
	}
	thenE, err := p.parseExprSingle()
	if err != nil {
		return nil, err
	}
	if _, err := p.lex.Expect(TokElse); err != nil {
		return nil, err
	}
	elseE, err := p.parseExprSingle()
	if err != nil {
		return nil, err
	}
	return &IfExpr{Cond: cond, Then: thenE, Else: elseE}, nil
}

// parseOrExpr ::= AndExpr ('or' AndExpr)*
func (p *parser) parseOrExpr() (Expr, error) {
	left, err := p.parseAndExpr()
	if err != nil {
		return nil, err
	}
	for p.lex.Peek().Kind == TokOr {
		p.lex.Next()
		right, err := p.parseAndExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "or", Left: left, Right: right}
	}
	return left, nil
}

// parseAndExpr ::= ComparisonExpr ('and' ComparisonExpr)*
func (p *parser) parseAndExpr() (Expr, error) {
	left, err := p.parseComparisonExpr()
	if err != nil {
		return nil, err
	}
	for p.lex.Peek().Kind == TokAnd {
		p.lex.Next()
		right, err := p.parseComparisonExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "and", Left: left, Right: right}
	}
	return left, nil
}

// parseComparisonExpr ::= RangeExpr ((ValueComp | GeneralComp | NodeComp) RangeExpr)?
func (p *parser) parseComparisonExpr() (Expr, error) {
	left, err := p.parseRangeExpr()
	if err != nil {
		return nil, err
	}
	op := ""
	switch p.lex.Peek().Kind {
	case TokEq:
		op = "="
	case TokNE:
		op = "!="
	case TokLT:
		op = "<"
	case TokLE:
		op = "<="
	case TokGT:
		op = ">"
	case TokGE:
		op = ">="
	case TokEqOp:
		op = "eq"
	case TokNeOp:
		op = "ne"
	case TokLtOp:
		op = "lt"
	case TokLeOp:
		op = "le"
	case TokGtOp:
		op = "gt"
	case TokGeOp:
		op = "ge"
	case TokIs:
		op = "is"
	case TokDLT:
		op = "<<"
	case TokDGT:
		op = ">>"
	}
	if op == "" {
		return left, nil
	}
	p.lex.Next()
	right, err := p.parseRangeExpr()
	if err != nil {
		return nil, err
	}
	return &BinaryExpr{Op: op, Left: left, Right: right}, nil
}

// parseRangeExpr ::= AdditiveExpr ('to' AdditiveExpr)?
func (p *parser) parseRangeExpr() (Expr, error) {
	left, err := p.parseAdditiveExpr()
	if err != nil {
		return nil, err
	}
	if p.lex.Peek().Kind == TokTo {
		p.lex.Next()
		right, err := p.parseAdditiveExpr()
		if err != nil {
			return nil, err
		}
		return &RangeExpr{Low: left, High: right}, nil
	}
	return left, nil
}

// parseAdditiveExpr ::= MultiplicativeExpr (('+' | '-') MultiplicativeExpr)*
func (p *parser) parseAdditiveExpr() (Expr, error) {
	left, err := p.parseMultiplicativeExpr()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.lex.Peek().Kind {
		case TokPlus:
			op = "+"
		case TokMinus:
			op = "-"
		}
		if op == "" {
			break
		}
		p.lex.Next()
		right, err := p.parseMultiplicativeExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

// parseMultiplicativeExpr ::= UnionExpr (('*' | 'div' | 'idiv' | 'mod') UnionExpr)*
func (p *parser) parseMultiplicativeExpr() (Expr, error) {
	left, err := p.parseUnionExpr()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.lex.Peek().Kind {
		case TokStar:
			// Only multiply if preceded by a non-path context (ambiguity with wildcard)
			// Heuristic: if left is a path/step, * could be a path extension — but
			// at this level we're past path expressions, so treat as multiply.
			op = "*"
		case TokDiv:
			op = "div"
		case TokIDiv:
			op = "idiv"
		case TokMod:
			op = "mod"
		}
		if op == "" {
			break
		}
		p.lex.Next()
		right, err := p.parseUnionExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

// parseUnionExpr ::= IntersectExceptExpr (('union' | '|') IntersectExceptExpr)*
func (p *parser) parseUnionExpr() (Expr, error) {
	left, err := p.parseIntersectExceptExpr()
	if err != nil {
		return nil, err
	}
	for p.lex.Peek().Kind == TokUnion || p.lex.Peek().Kind == TokPipe {
		p.lex.Next()
		right, err := p.parseIntersectExceptExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "union", Left: left, Right: right}
	}
	return left, nil
}

// parseIntersectExceptExpr ::= InstanceofExpr (('intersect'|'except') InstanceofExpr)*
func (p *parser) parseIntersectExceptExpr() (Expr, error) {
	left, err := p.parseInstanceofExpr()
	if err != nil {
		return nil, err
	}
	for p.lex.Peek().Kind == TokIntersect || p.lex.Peek().Kind == TokExcept {
		op := p.lex.Next().Value
		right, err := p.parseInstanceofExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseInstanceofExpr() (Expr, error) {
	e, err := p.parseTreatExpr()
	if err != nil {
		return nil, err
	}
	if p.lex.Peek().Kind == TokInstance {
		p.lex.Next() // instance
		if _, err := p.lex.Expect(TokOf); err != nil {
			return nil, err
		}
		typName := p.readTypeName()
		return &InstanceofExpr{Expr: e, TypeName: typName}, nil
	}
	return e, nil
}

func (p *parser) parseTreatExpr() (Expr, error) {
	e, err := p.parseCastableExpr()
	if err != nil {
		return nil, err
	}
	if p.lex.Peek().Kind == TokTreat {
		p.lex.Next()
		if _, err := p.lex.Expect(TokAs); err != nil {
			return nil, err
		}
		typName := p.readTypeName()
		_ = typName // treat as is a no-op in our implementation (we trust types)
	}
	return e, nil
}

func (p *parser) parseCastableExpr() (Expr, error) {
	e, err := p.parseCastExpr()
	if err != nil {
		return nil, err
	}
	if p.lex.Peek().Kind == TokCastable {
		p.lex.Next()
		if _, err := p.lex.Expect(TokAs); err != nil {
			return nil, err
		}
		typName := p.readTypeName()
		return &CastableExpr{Expr: e, TypeName: typName}, nil
	}
	return e, nil
}

func (p *parser) parseCastExpr() (Expr, error) {
	e, err := p.parseUnaryExpr()
	if err != nil {
		return nil, err
	}
	if p.lex.Peek().Kind == TokCast {
		p.lex.Next()
		if _, err := p.lex.Expect(TokAs); err != nil {
			return nil, err
		}
		typName := p.readTypeName()
		return &CastExpr{Expr: e, TypeName: typName}, nil
	}
	return e, nil
}

// readTypeName reads a (possibly qualified) type name like "xs:integer".
func (p *parser) readTypeName() string {
	t := p.lex.Next()
	name := t.Value
	if p.lex.Peek().Kind == TokColon {
		p.lex.Next()
		local := p.lex.Next()
		name = name + ":" + local.Value
	}
	// consume optional occurrence indicator
	if p.lex.Peek().Kind == TokQuestion || p.lex.Peek().Kind == TokStar || p.lex.Peek().Kind == TokPlus {
		p.lex.Next()
	}
	return name
}

// parseUnaryExpr ::= ('-' | '+')* ValueExpr
func (p *parser) parseUnaryExpr() (Expr, error) {
	if p.lex.Peek().Kind == TokMinus {
		p.lex.Next()
		e, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "-", Expr: e}, nil
	}
	if p.lex.Peek().Kind == TokPlus {
		p.lex.Next()
		return p.parseUnaryExpr()
	}
	return p.parsePathExpr()
}

// parsePathExpr handles / // and relative paths.
func (p *parser) parsePathExpr() (Expr, error) {
	switch p.lex.Peek().Kind {
	case TokSlash:
		p.lex.Next()
		rootStep := &Step{Axis: AxisRoot, NodeTest: &NodeTest{Kind: NodeTestKindAny}}
		if p.lex.Peek().Kind == TokEOF || isFollowToken(p.lex.Peek().Kind) {
			return &PathExpr{Steps: []*Step{rootStep}}, nil
		}
		path, err := p.parseRelativePath()
		if err != nil {
			return nil, err
		}
		path.Steps = append([]*Step{rootStep}, path.Steps...)
		return path, nil

	case TokDSlash:
		p.lex.Next()
		dsStep := &Step{Axis: AxisDescendantOrSelf, NodeTest: &NodeTest{Kind: NodeTestKindAny}}
		dsStep.DoubleSlash = true
		path, err := p.parseRelativePath()
		if err != nil {
			return nil, err
		}
		rootStep := &Step{Axis: AxisRoot, NodeTest: &NodeTest{Kind: NodeTestKindAny}}
		path.Steps = append([]*Step{rootStep, dsStep}, path.Steps...)
		return path, nil

	default:
		return p.parseRelativePath()
	}
}

// isFollowToken returns true for tokens that cannot start a relative path.
func isFollowToken(k TokenKind) bool {
	switch k {
	case TokRParen, TokRBrack, TokComma, TokEOF,
		TokAnd, TokOr, TokReturn, TokThen, TokElse, TokSatisfies,
		TokEq, TokNE, TokLT, TokLE, TokGT, TokGE,
		TokEqOp, TokNeOp, TokLtOp, TokLeOp, TokGtOp, TokGeOp,
		TokIs, TokDLT, TokDGT, TokTo,
		TokPlus, TokMinus, TokPipe, TokUnion, TokIntersect, TokExcept,
		TokDiv, TokIDiv, TokMod:
		return true
	}
	return false
}

// parseRelativePath ::= StepExpr (('/' | '//') StepExpr)*
func (p *parser) parseRelativePath() (*PathExpr, error) {
	first, err := p.parseStepExpr()
	if err != nil {
		return nil, err
	}
	path := &PathExpr{Steps: []*Step{first}}
	for {
		switch p.lex.Peek().Kind {
		case TokSlash:
			p.lex.Next()
			step, err := p.parseStepExpr()
			if err != nil {
				return nil, err
			}
			path.Steps = append(path.Steps, step)
		case TokDSlash:
			p.lex.Next()
			// // is shorthand for /descendant-or-self::node()/
			ds := &Step{
				Axis:        AxisDescendantOrSelf,
				NodeTest:    &NodeTest{Kind: NodeTestKindAny},
				DoubleSlash: true,
			}
			path.Steps = append(path.Steps, ds)
			step, err := p.parseStepExpr()
			if err != nil {
				return nil, err
			}
			path.Steps = append(path.Steps, step)
		default:
			return path, nil
		}
	}
}

// parseStepExpr ::= FilterExpr | AxisStep
func (p *parser) parseStepExpr() (*Step, error) {
	tok := p.lex.Peek()

	// Axis step detection: look for axis:: or abbreviated axes.
	if tok.Kind == TokAt {
		p.lex.Next()
		nt, err := p.parseNodeTest()
		if err != nil {
			return nil, err
		}
		preds, err := p.parsePredicates()
		if err != nil {
			return nil, err
		}
		return &Step{Axis: AxisAttribute, NodeTest: nt, Predicates: preds}, nil
	}
	if tok.Kind == TokDDot {
		p.lex.Next()
		preds, err := p.parsePredicates()
		if err != nil {
			return nil, err
		}
		return &Step{Axis: AxisParent, NodeTest: &NodeTest{Kind: NodeTestKindAny}, Predicates: preds}, nil
	}

	// Check for axis:: pattern: name '::'
	if tok.Kind == TokName {
		// Peek ahead for '::'
		// We need to speculatively check if this is an axis name.
		if axis, ok := axisName(tok.Value); ok {
			// Speculatively try to consume the axis + '::'.
			probe := &parser{lex: NewLexer(p.src[tok.Pos:])}
			probe.lex.Next() // consume axis name
			if probe.lex.Peek().Kind == TokDColon {
				// It's an axis step.
				p.lex.Next() // consume axis name
				p.lex.Next() // consume '::'
				nt, err := p.parseNodeTest()
				if err != nil {
					return nil, err
				}
				preds, err := p.parsePredicates()
				if err != nil {
					return nil, err
				}
				return &Step{Axis: axis, NodeTest: nt, Predicates: preds}, nil
			}
		}
	}

	// Check for KindTest (node(), text(), comment(), processing-instruction(), element(), attribute(), document-node())
	if tok.Kind == TokName && isKindTestName(tok.Value) {
		if p.peekIsLParen(tok.Value) {
			nt, err := p.parseKindTest()
			if err != nil {
				return nil, err
			}
			preds, err := p.parsePredicates()
			if err != nil {
				return nil, err
			}
			return &Step{Axis: AxisChild, NodeTest: nt, Predicates: preds}, nil
		}
	}

	// Check for name or wildcard (NameTest).
	if tok.Kind == TokName || tok.Kind == TokStar {
		// If name is followed by '(', it's a function call — treat as filter step.
		if tok.Kind == TokName && p.peekIsLParen(tok.Value) {
			primary, err := p.parsePrimaryExpr()
			if err != nil {
				return nil, err
			}
			preds, err := p.parsePredicates()
			if err != nil {
				return nil, err
			}
			return &Step{Primary: primary, Predicates: preds}, nil
		}
		nt, err := p.parseNodeTest()
		if err != nil {
			return nil, err
		}
		preds, err := p.parsePredicates()
		if err != nil {
			return nil, err
		}
		return &Step{Axis: AxisChild, NodeTest: nt, Predicates: preds}, nil
	}

	// Otherwise it must be a FilterExpr (primary + predicates).
	primary, err := p.parsePrimaryExpr()
	if err != nil {
		return nil, err
	}
	preds, err := p.parsePredicates()
	if err != nil {
		return nil, err
	}
	return &Step{Primary: primary, Predicates: preds}, nil
}


// peekIsLParen returns true if the next token after the current name is '('.
func (p *parser) peekIsLParen(name string) bool {
	_ = name
	cur := p.lex.Peek()
	sub := NewLexer(p.src[cur.Pos:])
	sub.Next() // consume the name
	return sub.Peek().Kind == TokLParen
}

func (p *parser) parseNodeTest() (*NodeTest, error) {
	tok := p.lex.Peek()

	// Wildcard *
	if tok.Kind == TokStar {
		p.lex.Next()
		return &NodeTest{Kind: NodeTestWild}, nil
	}

	if tok.Kind != TokName {
		return nil, fmt.Errorf("xpath: expected node test at pos %d, got %q", tok.Pos, tok.Value)
	}

	// KindTest?
	if isKindTestName(tok.Value) && p.peekIsLParen(tok.Value) {
		return p.parseKindTest()
	}

	// Name or QName or ns:*
	p.lex.Next()
	name := tok.Value
	if p.lex.Peek().Kind == TokColon {
		p.lex.Next()
		next := p.lex.Peek()
		if next.Kind == TokStar {
			p.lex.Next()
			return &NodeTest{Kind: NodeTestWild, Prefix: name}, nil
		}
		if next.Kind == TokName {
			local := p.lex.Next()
			return &NodeTest{Kind: NodeTestName, Prefix: name, Local: local.Value}, nil
		}
		return nil, fmt.Errorf("xpath: unexpected token after ':' in node test")
	}
	return &NodeTest{Kind: NodeTestName, Local: name}, nil
}

func isKindTestName(s string) bool {
	switch s {
	case "node", "text", "comment", "processing-instruction",
		"element", "attribute", "document-node", "schema-element", "schema-attribute":
		return true
	}
	return false
}

func (p *parser) parseKindTest() (*NodeTest, error) {
	name := p.lex.Next().Value // consume kind name
	if _, err := p.lex.Expect(TokLParen); err != nil {
		return nil, err
	}
	nt := &NodeTest{}
	switch name {
	case "node":
		nt.Kind = NodeTestKindAny
	case "text":
		nt.Kind = NodeTestKindText
	case "comment":
		nt.Kind = NodeTestKindComment
	case "processing-instruction":
		nt.Kind = NodeTestKindPI
		if p.lex.Peek().Kind == TokString {
			nt.PITarget = p.lex.Next().Value
		} else if p.lex.Peek().Kind == TokName {
			nt.PITarget = p.lex.Next().Value
		}
	case "document-node":
		nt.Kind = NodeTestKindDoc
		// optional element test inside — skip
		if p.lex.Peek().Kind != TokRParen {
			depth := 1
			for depth > 0 && p.lex.Peek().Kind != TokEOF {
				t := p.lex.Next()
				if t.Kind == TokLParen {
					depth++
				} else if t.Kind == TokRParen {
					depth--
				}
			}
			if _, err := p.lex.Expect(TokRParen); err != nil {
				return nil, err
			}
			return nt, nil
		}
	case "element":
		nt.Kind = NodeTestKindElem
		if p.lex.Peek().Kind != TokRParen {
			t := p.lex.Next()
			if t.Kind == TokStar {
				// element(*)
			} else {
				nt.Local = t.Value
				if p.lex.Peek().Kind == TokColon {
					p.lex.Next()
					loc := p.lex.Next()
					nt.Prefix = nt.Local
					nt.Local = loc.Value
				}
			}
			// skip optional type name
			if p.lex.Peek().Kind == TokComma {
				p.lex.Next()
				// consume type name
				p.lex.Next()
				if p.lex.Peek().Kind == TokColon {
					p.lex.Next()
					p.lex.Next()
				}
				if p.lex.Peek().Kind == TokQuestion {
					p.lex.Next()
				}
			}
		}
	case "attribute":
		nt.Kind = NodeTestKindAttr
		if p.lex.Peek().Kind != TokRParen {
			t := p.lex.Next()
			if t.Kind != TokStar {
				nt.Local = t.Value
				if p.lex.Peek().Kind == TokColon {
					p.lex.Next()
					loc := p.lex.Next()
					nt.Prefix = nt.Local
					nt.Local = loc.Value
				}
			}
		}
	default:
		nt.Kind = NodeTestKindAny
	}
	if _, err := p.lex.Expect(TokRParen); err != nil {
		return nil, err
	}
	return nt, nil
}

func (p *parser) parsePredicates() ([]Expr, error) {
	var preds []Expr
	for p.lex.Peek().Kind == TokLBrack {
		p.lex.Next() // [
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.lex.Expect(TokRBrack); err != nil {
			return nil, err
		}
		preds = append(preds, e)
	}
	return preds, nil
}

// parsePrimaryExpr ::= Literal | VarRef | '(' Expr ')' | '.' | FuncCall
func (p *parser) parsePrimaryExpr() (Expr, error) {
	tok := p.lex.Peek()
	switch tok.Kind {
	case TokInteger:
		p.lex.Next()
		v, _ := strconv.ParseInt(tok.Value, 10, 64)
		return &IntegerLit{Value: v}, nil
	case TokDecimal:
		p.lex.Next()
		v, _ := strconv.ParseFloat(tok.Value, 64)
		return &DecimalLit{Value: v}, nil
	case TokDouble:
		p.lex.Next()
		v, _ := strconv.ParseFloat(tok.Value, 64)
		return &DoubleLit{Value: v}, nil
	case TokString:
		p.lex.Next()
		return &StringLit{Value: tok.Value}, nil
	case TokDollar:
		p.lex.Next()
		name, err := p.lex.Expect(TokName)
		if err != nil {
			return nil, err
		}
		vname := name.Value
		prefix := ""
		if p.lex.Peek().Kind == TokColon {
			p.lex.Next()
			local, err := p.lex.Expect(TokName)
			if err != nil {
				return nil, err
			}
			prefix = vname
			vname = local.Value
		}
		return &VarRef{Prefix: prefix, Local: vname}, nil
	case TokLParen:
		p.lex.Next()
		if p.lex.Peek().Kind == TokRParen {
			p.lex.Next()
			return &SequenceExpr{Items: nil}, nil // empty sequence ()
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.lex.Expect(TokRParen); err != nil {
			return nil, err
		}
		return e, nil
	case TokDot:
		p.lex.Next()
		return &ContextItem{}, nil
	case TokName:
		// Could be function call or QName in a filter expression.
		// If the next-next token after any ':' is '(', it's a function call.
		return p.parseFuncCallOrName()
	}
	return nil, fmt.Errorf("xpath: unexpected token %q at pos %d in primary expression", tok.Value, tok.Pos)
}

func (p *parser) parseFuncCallOrName() (Expr, error) {
	name := p.lex.Next()
	prefix := ""
	local := name.Value

	if p.lex.Peek().Kind == TokColon {
		p.lex.Next()
		l2 := p.lex.Next()
		prefix = local
		local = l2.Value
	}

	if p.lex.Peek().Kind == TokLParen {
		p.lex.Next() // consume '('
		var args []Expr
		for p.lex.Peek().Kind != TokRParen {
			if len(args) > 0 {
				if _, err := p.lex.Expect(TokComma); err != nil {
					return nil, err
				}
			}
			arg, err := p.parseExprSingle()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
		}
		p.lex.Next() // consume ')'
		return &FuncCall{Prefix: prefix, Local: local, Args: args}, nil
	}

	// It's a name — treat as a name-test in a path step.
	// This shouldn't normally happen at the primary expression level,
	// but handle it gracefully.
	qname := local
	if prefix != "" {
		qname = prefix + ":" + local
	}
	return &StringLit{Value: qname}, nil
}

// axisName maps axis name strings to Axis constants.
func axisName(s string) (Axis, bool) {
	switch strings.ToLower(s) {
	case "child":
		return AxisChild, true
	case "descendant":
		return AxisDescendant, true
	case "attribute":
		return AxisAttribute, true
	case "self":
		return AxisSelf, true
	case "descendant-or-self":
		return AxisDescendantOrSelf, true
	case "following-sibling":
		return AxisFollowingSibling, true
	case "following":
		return AxisFollowing, true
	case "parent":
		return AxisParent, true
	case "ancestor":
		return AxisAncestor, true
	case "preceding-sibling":
		return AxisPrecedingSibling, true
	case "preceding":
		return AxisPreceding, true
	case "ancestor-or-self":
		return AxisAncestorOrSelf, true
	case "namespace":
		return AxisNamespace, true
	}
	return 0, false
}
