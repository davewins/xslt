package xpath

import (
	"fmt"
	"math"
	"sort"

	"github.com/davewins/xslt/dom"
)

// Eval evaluates a pre-parsed XPath expression against the given context.
func Eval(expr Expr, ctx *Context) (Sequence, error) {
	return evalExpr(expr, ctx)
}

// EvalString evaluates an XPath expression and returns its string value.
func EvalString(expr Expr, ctx *Context) (string, error) {
	seq, err := evalExpr(expr, ctx)
	if err != nil {
		return "", err
	}
	return StringValue(seq), nil
}

// EvalBool evaluates an XPath expression and returns its effective boolean value.
func EvalBool(expr Expr, ctx *Context) (bool, error) {
	seq, err := evalExpr(expr, ctx)
	if err != nil {
		return false, err
	}
	return BooleanValue(seq), nil
}

// EvalNodes evaluates an XPath expression and returns the result as DOM nodes.
func EvalNodes(expr Expr, ctx *Context) ([]*dom.Node, error) {
	seq, err := evalExpr(expr, ctx)
	if err != nil {
		return nil, err
	}
	return SeqToNodes(seq), nil
}

func evalExpr(e Expr, ctx *Context) (Sequence, error) {
	switch v := e.(type) {
	case *IntegerLit:
		return Sequence{IntegerItem(v.Value)}, nil
	case *DecimalLit:
		return Sequence{DecimalItem(v.Value)}, nil
	case *DoubleLit:
		return Sequence{DoubleItem(v.Value)}, nil
	case *StringLit:
		return Sequence{StringItem(v.Value)}, nil
	case *ContextItem:
		if ctx.Item == nil {
			return Sequence{}, nil
		}
		return Sequence{ctx.Item}, nil
	case *VarRef:
		val, ok := ctx.Vars[v.Local]
		if !ok {
			if v.Prefix != "" {
				key := v.Prefix + ":" + v.Local
				val, ok = ctx.Vars[key]
			}
		}
		if !ok {
			return Sequence{}, nil
		}
		return val, nil
	case *FuncCall:
		return evalFuncCall(v, ctx)
	case *PathExpr:
		return evalPath(v, ctx)
	case *FilterExpr:
		return evalFilter(v, ctx)
	case *Step:
		if v.Primary != nil {
			return evalFilter(&FilterExpr{Primary: v.Primary, Predicates: v.Predicates}, ctx)
		}
		return evalStep(v, ctx)
	case *BinaryExpr:
		return evalBinary(v, ctx)
	case *UnaryExpr:
		return evalUnary(v, ctx)
	case *SequenceExpr:
		return evalSequence(v, ctx)
	case *IfExpr:
		return evalIf(v, ctx)
	case *ForExpr:
		return evalFor(v, ctx)
	case *QuantifiedExpr:
		return evalQuantified(v, ctx)
	case *RangeExpr:
		return evalRange(v, ctx)
	case *InstanceofExpr:
		return evalInstanceof(v, ctx)
	case *CastExpr:
		return evalCast(v, ctx)
	case *CastableExpr:
		return evalCastable(v, ctx)
	}
	return nil, fmt.Errorf("xpath: unknown expression type %T", e)
}

func evalFuncCall(fc *FuncCall, ctx *Context) (Sequence, error) {
	fn, ok := ctx.LookupFunc(fc.Prefix, fc.Local)
	if !ok {
		return nil, fmt.Errorf("xpath: unknown function %s:%s", fc.Prefix, fc.Local)
	}
	args := make([]Sequence, len(fc.Args))
	for i, arg := range fc.Args {
		val, err := evalExpr(arg, ctx)
		if err != nil {
			return nil, err
		}
		args[i] = val
	}
	return fn(ctx, args)
}

func evalPath(path *PathExpr, ctx *Context) (Sequence, error) {
	if len(path.Steps) == 0 {
		return Sequence{}, nil
	}

	var current Sequence
	if ctx.Item != nil {
		current = Sequence{ctx.Item}
	}

	firstStep := true
	for _, step := range path.Steps {
		// Root navigation: move to document node.
		if step.Axis == AxisRoot {
			var next Sequence
			if ctx.Doc != nil {
				next = Sequence{&NodeItem{Node: ctx.Doc.Root}}
			} else {
				for _, item := range current {
					if item.IsNode() {
						root := item.(*NodeItem).Node
						for root.Parent != nil {
							root = root.Parent
						}
						next = Sequence{&NodeItem{Node: root}}
						break
					}
				}
			}
			current = next
			firstStep = false
			continue
		}

		// Filter step (primary expression): evaluate preserving non-node results.
		// The first filter step in a path uses the original ctx so that position()
		// and last() return the values established by the enclosing XSLT context
		// (e.g., xsl:for-each iteration). Subsequent filter steps re-contextualize
		// per item so that "." binds to each item in the sequence.
		if step.Primary != nil {
			if firstStep {
				seq, err := evalFilter(&FilterExpr{Primary: step.Primary, Predicates: step.Predicates}, ctx)
				if err != nil {
					return nil, err
				}
				current = seq
			} else {
				var next Sequence
				size := len(current)
				for i, item := range current {
					stepCtx := ctx.Sub(item, i+1, size)
					seq, err := evalFilter(&FilterExpr{Primary: step.Primary, Predicates: step.Predicates}, stepCtx)
					if err != nil {
						return nil, err
					}
					next = append(next, seq...)
				}
				current = next
			}
			firstStep = false
			continue
		}

		// Axis step: navigate from each node in current, collect unique results.
		nodeSet := make(map[int]*dom.Node)
		var nodeList []*dom.Node
		size := len(current)
		for i, item := range current {
			if !item.IsNode() {
				continue // axis navigation requires a node context
			}
			stepCtx := ctx.Sub(item, i+1, size)
			candidates := axisNodes(step.Axis, item.(*NodeItem).Node)
			filtered := filterByNodeTest(candidates, step.NodeTest, stepCtx)
			for _, n := range filtered {
				if _, seen := nodeSet[n.DocOrder]; !seen {
					nodeSet[n.DocOrder] = n
					nodeList = append(nodeList, n)
				}
			}
		}

		// Sort into document order (reverse axes use reverse document order).
		if !isReverseAxis(step.Axis) {
			NodeOrder(nodeList)
		}

		// Apply predicates.
		if len(step.Predicates) > 0 {
			var filtered []*dom.Node
			listSize := len(nodeList)
			for pos, n := range nodeList {
				match := true
				for _, pred := range step.Predicates {
					predCtx := ctx.Sub(&NodeItem{Node: n}, pos+1, listSize)
					seq, err := evalExpr(pred, predCtx)
					if err != nil {
						return nil, err
					}
					if len(seq) == 1 && !seq[0].IsNode() {
						av := seq[0].(*AtomicValue)
						if av.Kind == KindInteger || av.Kind == KindDecimal || av.Kind == KindDouble {
							if int(av.Num) != pos+1 {
								match = false
								break
							}
							continue
						}
					}
					if !BooleanValue(seq) {
						match = false
						break
					}
				}
				if match {
					filtered = append(filtered, n)
				}
			}
			nodeList = filtered
		}

		current = NodesToSeq(nodeList)
		firstStep = false
	}

	return current, nil
}

func evalAxisStep(step *Step, ctx *Context) ([]*dom.Node, error) {
	if step.Primary != nil {
		// Filter step — handled via evalFilter.
		seq, err := evalFilter(&FilterExpr{Primary: step.Primary, Predicates: step.Predicates}, ctx)
		if err != nil {
			return nil, err
		}
		return SeqToNodes(seq), nil
	}

	ctxNode := ctx.CurrentNode()
	if ctxNode == nil {
		return nil, nil
	}

	candidates := axisNodes(step.Axis, ctxNode)
	return filterByNodeTest(candidates, step.NodeTest, ctx), nil
}

// axisNodes returns the nodes on the given axis relative to n.
func axisNodes(axis Axis, n *dom.Node) []*dom.Node {
	switch axis {
	case AxisChild:
		return n.Children
	case AxisDescendant:
		return descendants(n, false)
	case AxisDescendantOrSelf:
		return descendants(n, true)
	case AxisAttribute:
		return n.Attributes
	case AxisSelf:
		return []*dom.Node{n}
	case AxisParent:
		if n.Parent != nil {
			return []*dom.Node{n.Parent}
		}
		return nil
	case AxisAncestor:
		return ancestors(n, false)
	case AxisAncestorOrSelf:
		return ancestors(n, true)
	case AxisFollowingSibling:
		return followingSiblings(n)
	case AxisPrecedingSibling:
		return precedingSiblings(n)
	case AxisFollowing:
		return following(n)
	case AxisPreceding:
		return preceding(n)
	case AxisNamespace:
		// Return namespace nodes for this element.
		var out []*dom.Node
		if n.Type == dom.NodeElement {
			for _, ns := range n.Namespaces {
				out = append(out, &dom.Node{
					Type:      dom.NodeNamespace,
					LocalName: ns.Prefix,
					Value:     ns.URI,
					Parent:    n,
					Document:  n.Document,
				})
			}
		}
		return out
	}
	return nil
}

func descendants(n *dom.Node, includeSelf bool) []*dom.Node {
	var out []*dom.Node
	if includeSelf {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, descendants(c, true)...)
	}
	return out
}

func ancestors(n *dom.Node, includeSelf bool) []*dom.Node {
	var out []*dom.Node
	if includeSelf {
		out = append(out, n)
	}
	cur := n.Parent
	for cur != nil {
		out = append(out, cur)
		cur = cur.Parent
	}
	return out
}

func followingSiblings(n *dom.Node) []*dom.Node {
	if n.Parent == nil {
		return nil
	}
	var out []*dom.Node
	found := false
	for _, s := range n.Parent.Children {
		if found {
			out = append(out, s)
		} else if s == n {
			found = true
		}
	}
	return out
}

func precedingSiblings(n *dom.Node) []*dom.Node {
	if n.Parent == nil {
		return nil
	}
	var out []*dom.Node
	for _, s := range n.Parent.Children {
		if s == n {
			break
		}
		out = append(out, s)
	}
	// Return in reverse document order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func following(n *dom.Node) []*dom.Node {
	var out []*dom.Node
	// Collect all nodes after n in document order.
	var after func(root *dom.Node, found *bool)
	after = func(node *dom.Node, found *bool) {
		if *found && node != n {
			out = append(out, node)
		}
		if node == n {
			*found = true
		}
		for _, c := range node.Children {
			after(c, found)
		}
	}
	root := n
	for root.Parent != nil {
		root = root.Parent
	}
	found := false
	after(root, &found)
	// Remove ancestors.
	anc := make(map[*dom.Node]bool)
	cur := n.Parent
	for cur != nil {
		anc[cur] = true
		cur = cur.Parent
	}
	var filtered []*dom.Node
	for _, node := range out {
		if !anc[node] {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

func preceding(n *dom.Node) []*dom.Node {
	var out []*dom.Node
	var before func(root *dom.Node) bool
	before = func(node *dom.Node) bool {
		if node == n {
			return true
		}
		out = append(out, node)
		for _, c := range node.Children {
			if before(c) {
				// Remove node itself from out (it was added then we found n).
				out = out[:len(out)-1]
				return true
			}
		}
		return false
	}
	root := n
	for root.Parent != nil {
		root = root.Parent
	}
	before(root)
	// Remove n and ancestors.
	anc := make(map[*dom.Node]bool)
	anc[n] = true
	cur := n.Parent
	for cur != nil {
		anc[cur] = true
		cur = cur.Parent
	}
	var filtered []*dom.Node
	for _, node := range out {
		if !anc[node] {
			filtered = append(filtered, node)
		}
	}
	// Return in reverse document order.
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered
}

// filterByNodeTest filters nodes by the given node test.
func filterByNodeTest(nodes []*dom.Node, nt *NodeTest, ctx *Context) []*dom.Node {
	if nt == nil {
		return nodes
	}
	var out []*dom.Node
	for _, n := range nodes {
		if nodeMatchesTest(n, nt, ctx) {
			out = append(out, n)
		}
	}
	return out
}

func nodeMatchesTest(n *dom.Node, nt *NodeTest, ctx *Context) bool {
	switch nt.Kind {
	case NodeTestKindAny:
		return n.Type != dom.NodeAttribute && n.Type != dom.NodeNamespace

	case NodeTestKindText:
		return n.Type == dom.NodeText

	case NodeTestKindComment:
		return n.Type == dom.NodeComment

	case NodeTestKindPI:
		if n.Type != dom.NodeProcessingInstruction {
			return false
		}
		return nt.PITarget == "" || n.LocalName == nt.PITarget

	case NodeTestKindDoc:
		return n.Type == dom.NodeDocument

	case NodeTestKindElem:
		if n.Type != dom.NodeElement {
			return false
		}
		if nt.Local == "" {
			return true
		}
		if nt.Prefix != "" {
			ns := ctx.ResolveNamespace(nt.Prefix)
			return n.LocalName == nt.Local && n.NamespaceURI == ns
		}
		return n.LocalName == nt.Local

	case NodeTestKindAttr:
		if n.Type != dom.NodeAttribute {
			return false
		}
		if nt.Local == "" {
			return true
		}
		return n.LocalName == nt.Local

	case NodeTestWild:
		if nt.Prefix != "" {
			// ns:*  — match any node with this namespace.
			ns := ctx.ResolveNamespace(nt.Prefix)
			return (n.Type == dom.NodeElement || n.Type == dom.NodeAttribute) && n.NamespaceURI == ns
		}
		return n.Type == dom.NodeElement || n.Type == dom.NodeAttribute

	case NodeTestName:
		if n.Type != dom.NodeElement && n.Type != dom.NodeAttribute {
			return false
		}
		if nt.Prefix != "" {
			ns := ctx.ResolveNamespace(nt.Prefix)
			return n.LocalName == nt.Local && n.NamespaceURI == ns
		}
		return n.LocalName == nt.Local
	}
	return false
}

func isReverseAxis(a Axis) bool {
	switch a {
	case AxisParent, AxisAncestor, AxisAncestorOrSelf, AxisPrecedingSibling, AxisPreceding:
		return true
	}
	return false
}

// evalStep handles a Step that may be a standalone expression.
func evalStep(step *Step, ctx *Context) (Sequence, error) {
	nodes, err := evalAxisStep(step, ctx)
	if err != nil {
		return nil, err
	}
	if len(step.Predicates) > 0 {
		var filtered []*dom.Node
		size := len(nodes)
		for pos, n := range nodes {
			match := true
			for _, pred := range step.Predicates {
				predCtx := ctx.Sub(&NodeItem{Node: n}, pos+1, size)
				seq, err := evalExpr(pred, predCtx)
				if err != nil {
					return nil, err
				}
				if len(seq) == 1 && !seq[0].IsNode() {
					av := seq[0].(*AtomicValue)
					if av.Kind == KindInteger || av.Kind == KindDecimal || av.Kind == KindDouble {
						if int(av.Num) != pos+1 {
							match = false
							break
						}
						continue
					}
				}
				if !BooleanValue(seq) {
					match = false
					break
				}
			}
			if match {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}
	return NodesToSeq(nodes), nil
}

func evalFilter(fe *FilterExpr, ctx *Context) (Sequence, error) {
	base, err := evalExpr(fe.Primary, ctx)
	if err != nil {
		return nil, err
	}
	for _, pred := range fe.Predicates {
		var filtered Sequence
		size := len(base)
		for pos, item := range base {
			predCtx := ctx.Sub(item, pos+1, size)
			seq, err := evalExpr(pred, predCtx)
			if err != nil {
				return nil, err
			}
			if len(seq) == 1 && !seq[0].IsNode() {
				av := seq[0].(*AtomicValue)
				if av.Kind == KindInteger || av.Kind == KindDecimal || av.Kind == KindDouble {
					if int(av.Num) == pos+1 {
						filtered = append(filtered, item)
					}
					continue
				}
			}
			if BooleanValue(seq) {
				filtered = append(filtered, item)
			}
		}
		base = filtered
	}
	return base, nil
}

func evalBinary(b *BinaryExpr, ctx *Context) (Sequence, error) {
	switch b.Op {
	case "or":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		if BooleanValue(left) {
			return Sequence{BoolItem(true)}, nil
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		return Sequence{BoolItem(BooleanValue(right))}, nil

	case "and":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		if !BooleanValue(left) {
			return Sequence{BoolItem(false)}, nil
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		return Sequence{BoolItem(BooleanValue(right))}, nil

	case "=", "!=", "<", "<=", ">", ">=":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		return Sequence{BoolItem(GeneralCompare(left, right, b.Op))}, nil

	case "eq", "ne", "lt", "le", "gt", "ge":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		result, err := ValueCompare(left, right, b.Op)
		if err != nil {
			return nil, err
		}
		return Sequence{BoolItem(result)}, nil

	case "is":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		if len(left) == 0 || len(right) == 0 {
			return Sequence{BoolItem(false)}, nil
		}
		if left[0].IsNode() && right[0].IsNode() {
			return Sequence{BoolItem(left[0].(*NodeItem).Node == right[0].(*NodeItem).Node)}, nil
		}
		return Sequence{BoolItem(false)}, nil

	case "<<", ">>":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		if len(left) == 0 || len(right) == 0 {
			return Sequence{BoolItem(false)}, nil
		}
		if left[0].IsNode() && right[0].IsNode() {
			lOrder := left[0].(*NodeItem).Node.DocOrder
			rOrder := right[0].(*NodeItem).Node.DocOrder
			if b.Op == "<<" {
				return Sequence{BoolItem(lOrder < rOrder)}, nil
			}
			return Sequence{BoolItem(lOrder > rOrder)}, nil
		}
		return Sequence{BoolItem(false)}, nil

	case "+", "-", "*", "div", "idiv", "mod":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		lf := NumberValue(left)
		rf := NumberValue(right)
		var result float64
		switch b.Op {
		case "+":
			result = lf + rf
		case "-":
			result = lf - rf
		case "*":
			result = lf * rf
		case "div":
			if rf == 0 {
				if lf == 0 {
					result = math.NaN()
				} else if lf > 0 {
					result = math.Inf(1)
				} else {
					result = math.Inf(-1)
				}
			} else {
				result = lf / rf
			}
		case "idiv":
			if rf == 0 {
				return nil, fmt.Errorf("xpath: division by zero in idiv")
			}
			result = math.Trunc(lf / rf)
		case "mod":
			if rf == 0 {
				result = math.NaN()
			} else {
				result = math.Mod(lf, rf)
			}
		}
		return Sequence{DoubleItem(result)}, nil

	case "union":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		seen := make(map[int]bool)
		var nodes []*dom.Node
		for _, item := range append(left, right...) {
			if item.IsNode() {
				n := item.(*NodeItem).Node
				if !seen[n.DocOrder] {
					seen[n.DocOrder] = true
					nodes = append(nodes, n)
				}
			}
		}
		NodeOrder(nodes)
		return NodesToSeq(nodes), nil

	case "intersect":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		rightSet := make(map[int]bool)
		for _, item := range right {
			if item.IsNode() {
				rightSet[item.(*NodeItem).Node.DocOrder] = true
			}
		}
		var nodes []*dom.Node
		seen := make(map[int]bool)
		for _, item := range left {
			if item.IsNode() {
				n := item.(*NodeItem).Node
				if rightSet[n.DocOrder] && !seen[n.DocOrder] {
					seen[n.DocOrder] = true
					nodes = append(nodes, n)
				}
			}
		}
		NodeOrder(nodes)
		return NodesToSeq(nodes), nil

	case "except":
		left, err := evalExpr(b.Left, ctx)
		if err != nil {
			return nil, err
		}
		right, err := evalExpr(b.Right, ctx)
		if err != nil {
			return nil, err
		}
		rightSet := make(map[int]bool)
		for _, item := range right {
			if item.IsNode() {
				rightSet[item.(*NodeItem).Node.DocOrder] = true
			}
		}
		var nodes []*dom.Node
		seen := make(map[int]bool)
		for _, item := range left {
			if item.IsNode() {
				n := item.(*NodeItem).Node
				if !rightSet[n.DocOrder] && !seen[n.DocOrder] {
					seen[n.DocOrder] = true
					nodes = append(nodes, n)
				}
			}
		}
		NodeOrder(nodes)
		return NodesToSeq(nodes), nil
	}

	return nil, fmt.Errorf("xpath: unknown binary operator %q", b.Op)
}

func evalUnary(u *UnaryExpr, ctx *Context) (Sequence, error) {
	seq, err := evalExpr(u.Expr, ctx)
	if err != nil {
		return nil, err
	}
	f := NumberValue(seq)
	if u.Op == "-" {
		f = -f
	}
	return Sequence{DoubleItem(f)}, nil
}

func evalSequence(s *SequenceExpr, ctx *Context) (Sequence, error) {
	if len(s.Items) == 0 {
		return Sequence{}, nil
	}
	var out Sequence
	for _, item := range s.Items {
		seq, err := evalExpr(item, ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, seq...)
	}
	return out, nil
}

func evalIf(e *IfExpr, ctx *Context) (Sequence, error) {
	cond, err := evalExpr(e.Cond, ctx)
	if err != nil {
		return nil, err
	}
	if BooleanValue(cond) {
		return evalExpr(e.Then, ctx)
	}
	return evalExpr(e.Else, ctx)
}

func evalFor(e *ForExpr, ctx *Context) (Sequence, error) {
	in, err := evalExpr(e.In, ctx)
	if err != nil {
		return nil, err
	}
	var out Sequence
	for _, item := range in {
		bodyCtx := ctx.WithVar(e.Var, Sequence{item})
		seq, err := evalExpr(e.Body, bodyCtx)
		if err != nil {
			return nil, err
		}
		out = append(out, seq...)
	}
	return out, nil
}

func evalQuantified(e *QuantifiedExpr, ctx *Context) (Sequence, error) {
	in, err := evalExpr(e.In, ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range in {
		bodyCtx := ctx.WithVar(e.Var, Sequence{item})
		seq, err := evalExpr(e.Body, bodyCtx)
		if err != nil {
			return nil, err
		}
		result := BooleanValue(seq)
		if e.Kind == "some" && result {
			return Sequence{BoolItem(true)}, nil
		}
		if e.Kind == "every" && !result {
			return Sequence{BoolItem(false)}, nil
		}
	}
	if e.Kind == "some" {
		return Sequence{BoolItem(false)}, nil
	}
	return Sequence{BoolItem(true)}, nil
}

func evalRange(e *RangeExpr, ctx *Context) (Sequence, error) {
	low, err := evalExpr(e.Low, ctx)
	if err != nil {
		return nil, err
	}
	high, err := evalExpr(e.High, ctx)
	if err != nil {
		return nil, err
	}
	l := int64(NumberValue(low))
	h := int64(NumberValue(high))
	if l > h {
		return Sequence{}, nil
	}
	if ctx.MaxRangeSize > 0 && h-l+1 > int64(ctx.MaxRangeSize) {
		return nil, fmt.Errorf("xpath: range size %d exceeds limit of %d", h-l+1, ctx.MaxRangeSize)
	}
	out := make(Sequence, 0, h-l+1)
	for i := l; i <= h; i++ {
		out = append(out, IntegerItem(i))
	}
	return out, nil
}

func evalInstanceof(e *InstanceofExpr, ctx *Context) (Sequence, error) {
	seq, err := evalExpr(e.Expr, ctx)
	if err != nil {
		return nil, err
	}
	// Simplified: check if all items match the type.
	_ = seq
	return Sequence{BoolItem(true)}, nil
}

func evalCast(e *CastExpr, ctx *Context) (Sequence, error) {
	seq, err := evalExpr(e.Expr, ctx)
	if err != nil {
		return nil, err
	}
	if len(seq) == 0 {
		return seq, nil
	}
	typeName := e.TypeName
	switch {
	case typeName == "xs:string" || typeName == "string":
		return Sequence{StringItem(seq[0].StringValue())}, nil
	case typeName == "xs:integer" || typeName == "integer":
		f, _ := TypedNumber(seq[0])
		return Sequence{IntegerItem(int64(f))}, nil
	case typeName == "xs:double" || typeName == "double" || typeName == "xs:float" || typeName == "float":
		f, _ := TypedNumber(seq[0])
		return Sequence{DoubleItem(f)}, nil
	case typeName == "xs:boolean" || typeName == "boolean":
		return Sequence{BoolItem(BooleanValue(seq))}, nil
	case typeName == "xs:decimal" || typeName == "decimal":
		f, _ := TypedNumber(seq[0])
		return Sequence{DecimalItem(f)}, nil
	}
	return seq, nil
}

func evalCastable(e *CastableExpr, ctx *Context) (Sequence, error) {
	// Simplified: always return true.
	return Sequence{BoolItem(true)}, nil
}

// SortNodes sorts a list of (node, key) pairs by key value.
func SortNodes(nodes []*dom.Node, keyFn func(*dom.Node) string, order string, caseOrder string, dataType string) []*dom.Node {
	type pair struct {
		node   *dom.Node
		key    string
		numKey float64
	}
	pairs := make([]pair, len(nodes))
	isNum := dataType == "number"
	for i, n := range nodes {
		k := keyFn(n)
		numK := 0.0
		if isNum {
			numK, _ = strToNumber(k)
		}
		pairs[i] = pair{n, k, numK}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		var less bool
		if isNum {
			less = a.numKey < b.numKey
		} else {
			less = a.key < b.key
		}
		if order == "descending" {
			less = !less
		}
		return less
	})
	out := make([]*dom.Node, len(nodes))
	for i, p := range pairs {
		out[i] = p.node
	}
	return out
}
