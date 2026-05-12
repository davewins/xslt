package xpath

// Expr is the interface implemented by all XPath AST nodes.
type Expr interface {
	exprNode()
}

// marker
func (*IntegerLit) exprNode()     {}
func (*DecimalLit) exprNode()     {}
func (*DoubleLit) exprNode()      {}
func (*StringLit) exprNode()      {}
func (*VarRef) exprNode()         {}
func (*ContextItem) exprNode()    {}
func (*FuncCall) exprNode()       {}
func (*PathExpr) exprNode()       {}
func (*Step) exprNode()           {}
func (*FilterExpr) exprNode()     {}
func (*BinaryExpr) exprNode()     {}
func (*UnaryExpr) exprNode()      {}
func (*SequenceExpr) exprNode()   {}
func (*IfExpr) exprNode()         {}
func (*ForExpr) exprNode()        {}
func (*QuantifiedExpr) exprNode() {}
func (*RangeExpr) exprNode()      {}
func (*InstanceofExpr) exprNode() {}
func (*CastExpr) exprNode()       {}
func (*CastableExpr) exprNode()   {}

// Literals
type IntegerLit struct{ Value int64 }
type DecimalLit struct{ Value float64 }
type DoubleLit struct{ Value float64 }
type StringLit struct{ Value string }

// VarRef is $name.
type VarRef struct {
	Prefix string
	Local  string
}

// ContextItem is the '.' expression.
type ContextItem struct{}

// FuncCall is ns:name(args...).
type FuncCall struct {
	Prefix string
	Local  string
	Args   []Expr
}

// PathExpr is a chain of steps separated by / or //.
// Leading slash is represented by a special first step.
type PathExpr struct {
	Steps []*Step // len >= 1
}

// Axis constants.
type Axis int

const (
	AxisChild Axis = iota
	AxisDescendant
	AxisAttribute
	AxisSelf
	AxisDescendantOrSelf
	AxisFollowingSibling
	AxisFollowing
	AxisParent
	AxisAncestor
	AxisPrecedingSibling
	AxisPreceding
	AxisAncestorOrSelf
	AxisNamespace
	AxisRoot // internal: "/" at the start of a path
)

// NodeTestKind classifies a NodeTest.
type NodeTestKind int

const (
	NodeTestName    NodeTestKind = iota // element-name or @attr-name
	NodeTestWild                        // * or ns:*
	NodeTestKindAny                     // node()
	NodeTestKindText                    // text()
	NodeTestKindComment                 // comment()
	NodeTestKindPI                      // processing-instruction(name?)
	NodeTestKindDoc                     // document-node(element-test?)
	NodeTestKindElem                    // element(name?, type?)
	NodeTestKindAttr                    // attribute(name?, type?)
)

// NodeTest is the node-test part of an axis step.
type NodeTest struct {
	Kind      NodeTestKind
	Prefix    string // for NameTest or ns:* wildcard
	Local     string // for NameTest; empty means wildcard
	PITarget  string // for processing-instruction(target)
}

// Step is a single step in a path expression.
type Step struct {
	Axis       Axis
	DoubleSlash bool     // step was preceded by // (implies descendant-or-self::node() step)
	NodeTest   *NodeTest
	Predicates []Expr
	// For filter steps (primary expression with predicates):
	Primary    Expr
}

// FilterExpr is a primary expression followed by predicates.
type FilterExpr struct {
	Primary    Expr
	Predicates []Expr
}

// BinaryExpr covers all binary operators.
type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
}

// UnaryExpr is -expr or +expr.
type UnaryExpr struct {
	Op   string
	Expr Expr
}

// SequenceExpr is expr, expr, ...
type SequenceExpr struct {
	Items []Expr
}

// IfExpr is if (Cond) then Then else Else.
type IfExpr struct {
	Cond Expr
	Then Expr
	Else Expr
}

// ForExpr is for $v in E return body.
type ForExpr struct {
	Var  string
	In   Expr
	Body Expr
}

// QuantifiedExpr is some/every $v in E satisfies body.
type QuantifiedExpr struct {
	Kind string // "some" | "every"
	Var  string
	In   Expr
	Body Expr
}

// RangeExpr is low to high.
type RangeExpr struct {
	Low  Expr
	High Expr
}

// InstanceofExpr is expr instance of type.
type InstanceofExpr struct {
	Expr     Expr
	TypeName string
}

// CastExpr is expr cast as type.
type CastExpr struct {
	Expr     Expr
	TypeName string
}

// CastableExpr is expr castable as type.
type CastableExpr struct {
	Expr     Expr
	TypeName string
}
