package xpath

import (
	"fmt"
	"strings"

	"github.com/davewins/xslt/dom"
)

// Pattern represents a compiled XSLT match pattern.
type Pattern struct {
	raw  string
	alts []patternAlt // union alternatives
}

type patternAlt struct {
	steps []patternStep
}

type patternStep struct {
	axis     Axis
	nodeTest *NodeTest
	preds    []Expr
}

// CompilePattern compiles an XSLT match pattern string.
// Patterns are a restricted subset of XPath location paths.
func CompilePattern(pat string) (*Pattern, error) {
	p := &Pattern{raw: pat}
	// Split on '|' for union patterns.
	for _, alt := range splitUnion(pat) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		a, err := parsePatternAlt(alt)
		if err != nil {
			return nil, err
		}
		p.alts = append(p.alts, a)
	}
	if len(p.alts) == 0 {
		return nil, fmt.Errorf("pattern: empty pattern %q", pat)
	}
	return p, nil
}

// splitUnion splits a pattern on top-level '|' (not inside brackets).
func splitUnion(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
		case '(':
			depth++
		case ')':
			depth--
		case '|':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func parsePatternAlt(s string) (patternAlt, error) {
	s = strings.TrimSpace(s)
	var alt patternAlt

	// Special cases.
	if s == "/" {
		alt.steps = []patternStep{{axis: AxisRoot, nodeTest: &NodeTest{Kind: NodeTestKindDoc}}}
		return alt, nil
	}

	// Strip leading / or //.
	isAbsolute := false
	isDescendant := false
	if strings.HasPrefix(s, "//") {
		s = s[2:]
		isAbsolute = true
		isDescendant = true
	} else if strings.HasPrefix(s, "/") {
		s = s[1:]
		isAbsolute = true
	}

	// Parse the path steps.
	steps, err := parsePatternPath(s)
	if err != nil {
		return alt, err
	}

	if isAbsolute {
		if isDescendant {
			// //foo means: root → descendant-or-self::node() → foo
			rootStep := patternStep{axis: AxisRoot, nodeTest: &NodeTest{Kind: NodeTestKindDoc}}
			dsStep := patternStep{axis: AxisDescendantOrSelf, nodeTest: &NodeTest{Kind: NodeTestKindAny}}
			alt.steps = append([]patternStep{rootStep, dsStep}, steps...)
		} else {
			rootStep := patternStep{axis: AxisRoot, nodeTest: &NodeTest{Kind: NodeTestKindDoc}}
			alt.steps = append([]patternStep{rootStep}, steps...)
		}
	} else {
		alt.steps = steps
	}

	return alt, nil
}

func parsePatternPath(s string) ([]patternStep, error) {
	var steps []patternStep
	// Split on / taking care of predicates.
	segments := splitPath(s)
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if seg == ".." {
			steps = append(steps, patternStep{
				axis:     AxisParent,
				nodeTest: &NodeTest{Kind: NodeTestKindAny},
			})
			continue
		}
		if seg == "." {
			steps = append(steps, patternStep{
				axis:     AxisSelf,
				nodeTest: &NodeTest{Kind: NodeTestKindAny},
			})
			continue
		}

		// Extract predicates.
		preds, seg, err := extractPredicates(seg)
		if err != nil {
			return nil, err
		}

		// Determine axis.
		axis := AxisChild
		nodeTestStr := seg

		if strings.HasPrefix(seg, "@") {
			axis = AxisAttribute
			nodeTestStr = seg[1:]
		} else if idx := strings.Index(seg, "::"); idx >= 0 {
			axisName := seg[:idx]
			nodeTestStr = seg[idx+2:]
			if a, ok := axisNameMap[strings.ToLower(axisName)]; ok {
				axis = a
			}
		}

		nt, err := parsePatternNodeTest(nodeTestStr)
		if err != nil {
			return nil, err
		}
		steps = append(steps, patternStep{axis: axis, nodeTest: nt, preds: preds})
	}
	return steps, nil
}

var axisNameMap = map[string]Axis{
	"child":                AxisChild,
	"descendant":           AxisDescendant,
	"attribute":            AxisAttribute,
	"self":                 AxisSelf,
	"descendant-or-self":   AxisDescendantOrSelf,
	"following-sibling":    AxisFollowingSibling,
	"following":            AxisFollowing,
	"parent":               AxisParent,
	"ancestor":             AxisAncestor,
	"preceding-sibling":    AxisPrecedingSibling,
	"preceding":            AxisPreceding,
	"ancestor-or-self":     AxisAncestorOrSelf,
	"namespace":            AxisNamespace,
}

func splitPath(s string) []string {
	var parts []string
	depth := 0
	start := 0
	i := 0
	for i < len(s) {
		ch := s[i]
		switch ch {
		case '[':
			depth++
			i++
		case ']':
			depth--
			i++
		case '(':
			depth++
			i++
		case ')':
			depth--
			i++
		case '/':
			if depth == 0 {
				if i+1 < len(s) && s[i+1] == '/' {
					parts = append(parts, s[start:i])
					// Insert implicit descendant-or-self step.
					parts = append(parts, "descendant-or-self::node()")
					i += 2
					start = i
				} else {
					parts = append(parts, s[start:i])
					i++
					start = i
				}
			} else {
				i++
			}
		default:
			i++
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func extractPredicates(s string) ([]Expr, string, error) {
	var preds []Expr
	for {
		idx := strings.Index(s, "[")
		if idx < 0 {
			break
		}
		// Find matching ']'.
		depth := 0
		end := -1
		for i := idx; i < len(s); i++ {
			if s[i] == '[' {
				depth++
			} else if s[i] == ']' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			return nil, s, fmt.Errorf("pattern: unmatched '[' in %q", s)
		}
		predExpr := s[idx+1 : end]
		e, err := Parse(predExpr)
		if err != nil {
			return nil, s, fmt.Errorf("pattern: predicate parse error: %w", err)
		}
		preds = append(preds, e)
		s = s[:idx] + s[end+1:]
	}
	return preds, s, nil
}

func parsePatternNodeTest(s string) (*NodeTest, error) {
	s = strings.TrimSpace(s)
	if s == "*" {
		return &NodeTest{Kind: NodeTestWild}, nil
	}
	if s == "." {
		return &NodeTest{Kind: NodeTestKindAny}, nil
	}
	// Kind tests
	if strings.HasSuffix(s, "()") {
		name := s[:len(s)-2]
		switch name {
		case "node":
			return &NodeTest{Kind: NodeTestKindAny}, nil
		case "text":
			return &NodeTest{Kind: NodeTestKindText}, nil
		case "comment":
			return &NodeTest{Kind: NodeTestKindComment}, nil
		case "processing-instruction":
			return &NodeTest{Kind: NodeTestKindPI}, nil
		case "document-node":
			return &NodeTest{Kind: NodeTestKindDoc}, nil
		case "element":
			return &NodeTest{Kind: NodeTestKindElem}, nil
		case "attribute":
			return &NodeTest{Kind: NodeTestKindAttr}, nil
		}
	}
	// processing-instruction with target
	if strings.HasPrefix(s, "processing-instruction(") && strings.HasSuffix(s, ")") {
		target := s[len("processing-instruction(") : len(s)-1]
		target = strings.Trim(target, `'"`)
		return &NodeTest{Kind: NodeTestKindPI, PITarget: target}, nil
	}

	// QName or ns:* wildcard
	if idx := strings.Index(s, ":"); idx >= 0 {
		pre := s[:idx]
		local := s[idx+1:]
		if local == "*" {
			return &NodeTest{Kind: NodeTestWild, Prefix: pre}, nil
		}
		return &NodeTest{Kind: NodeTestName, Prefix: pre, Local: local}, nil
	}
	return &NodeTest{Kind: NodeTestName, Local: s}, nil
}

// Matches returns true if node n matches this pattern.
// nsMap is used to resolve namespace prefixes in the pattern.
func (p *Pattern) Matches(n *dom.Node, ctx *Context) bool {
	for _, alt := range p.alts {
		if matchAlt(alt, n, ctx) {
			return true
		}
	}
	return false
}

func matchAlt(alt patternAlt, n *dom.Node, ctx *Context) bool {
	steps := alt.steps
	if len(steps) == 0 {
		return false
	}

	// Match from the last step backward through the node's ancestors.
	return matchSteps(steps, n, ctx)
}

// matchSteps matches a node against a sequence of pattern steps (right to left).
func matchSteps(steps []patternStep, n *dom.Node, ctx *Context) bool {
	last := steps[len(steps)-1]
	rest := steps[:len(steps)-1]

	// Check if n matches the last step.
	if !stepMatchesNode(last, n, ctx) {
		return false
	}

	if len(rest) == 0 {
		return true
	}

	prevStep := rest[len(rest)-1]

	switch prevStep.axis {
	case AxisRoot:
		// n must be a child of the document root.
		parent := n.Parent
		if parent == nil {
			return false
		}
		return parent.Type == dom.NodeDocument && len(rest) == 1

	case AxisChild:
		// n's parent must match the previous step chain.
		if n.Parent == nil {
			return false
		}
		return matchSteps(rest, n.Parent, ctx)

	case AxisDescendant:
		// Any ancestor of n must match the rest.
		anc := n.Parent
		for anc != nil {
			if matchSteps(rest, anc, ctx) {
				return true
			}
			anc = anc.Parent
		}
		return false

	case AxisDescendantOrSelf:
		// n itself or any ancestor must match.
		cur := n
		for cur != nil {
			if matchSteps(rest, cur, ctx) {
				return true
			}
			cur = cur.Parent
		}
		return false

	case AxisParent:
		if n.Parent == nil {
			return false
		}
		return matchSteps(rest, n.Parent, ctx)

	case AxisAttribute:
		// n must be an attribute, handled in stepMatchesNode above.
		return true
	}

	return false
}

func stepMatchesNode(step patternStep, n *dom.Node, ctx *Context) bool {
	// First check the node test.
	if !patternNodeTestMatches(step.nodeTest, n, ctx) {
		return false
	}

	// Then evaluate predicates.
	if len(step.preds) == 0 {
		return true
	}

	// For predicates in patterns, we need to evaluate them with the node as context.
	// The position/size context is tricky — we use the node's actual sibling position.
	size := 1
	pos := 1
	if n.Parent != nil {
		siblings := n.Parent.Children
		for i, s := range siblings {
			if s.Type == n.Type && s.LocalName == n.LocalName && s.NamespaceURI == n.NamespaceURI {
				if s == n {
					pos = i + 1
				}
				size = i + 1
			}
		}
	}

	predCtx := ctx.Sub(&NodeItem{Node: n}, pos, size)
	for _, pred := range step.preds {
		seq, err := evalExpr(pred, predCtx)
		if err != nil {
			return false
		}
		if len(seq) == 1 && !seq[0].IsNode() {
			av := seq[0].(*AtomicValue)
			if av.Kind == KindInteger || av.Kind == KindDecimal || av.Kind == KindDouble {
				if int(av.Num) != pos {
					return false
				}
				continue
			}
		}
		if !BooleanValue(seq) {
			return false
		}
	}
	return true
}

func patternNodeTestMatches(nt *NodeTest, n *dom.Node, ctx *Context) bool {
	if nt == nil {
		return true
	}
	return nodeMatchesTest(n, nt, ctx)
}

// Priority returns the default priority for this pattern per XSLT 2.0 spec.
func (p *Pattern) Priority() float64 {
	if len(p.alts) == 0 {
		return 0
	}
	// Return the maximum priority across all alternatives.
	max := altPriority(p.alts[0])
	for _, alt := range p.alts[1:] {
		if v := altPriority(alt); v > max {
			max = v
		}
	}
	return max
}

func altPriority(alt patternAlt) float64 {
	if len(alt.steps) == 0 {
		return 0
	}
	last := alt.steps[len(alt.steps)-1]
	if len(alt.steps) > 1 {
		return 0.5 // path patterns have priority 0.5
	}
	if len(last.preds) > 0 {
		return 0.5 // predicates have priority 0.5
	}
	switch last.nodeTest.Kind {
	case NodeTestKindAny, NodeTestKindText, NodeTestKindComment, NodeTestKindPI:
		if last.nodeTest.PITarget != "" {
			return 0
		}
		return -0.5 // wildcard kinds
	case NodeTestWild:
		if last.nodeTest.Prefix != "" {
			return -0.25 // ns:* wildcard
		}
		return -0.5 // pure * wildcard
	case NodeTestName, NodeTestKindElem, NodeTestKindAttr:
		if last.nodeTest.Local != "" {
			return 0 // exact name match
		}
		return -0.5
	case NodeTestKindDoc:
		return -0.5
	}
	return 0
}

// Raw returns the original pattern string.
func (p *Pattern) Raw() string {
	return p.raw
}
