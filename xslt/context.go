package xslt

import (
	"github.com/davewins/xslt/dom"
	"github.com/davewins/xslt/xpath"
)

// TransformContext holds the full XSLT transformation state.
type TransformContext struct {
	Stylesheet   *Stylesheet
	SourceDoc    *dom.Document
	ResultDoc    *dom.Document
	XPathCtx     *xpath.Context
	Mode         string
	CurrentNode  *dom.Node   // xsl:current()
	CurrentGroup xpath.Sequence // for xsl:for-each-group
	CurrentGroupKey string
	Params       map[string]xpath.Sequence // tunnel params
	Keys         map[string]map[string][]*dom.Node // key name → value → nodes (built lazily)
	Messages     []string
}

// NewTransformContext creates a new transformation context.
func NewTransformContext(ss *Stylesheet, sourceDoc *dom.Document) *TransformContext {
	xctx := xpath.NewContext(sourceDoc)
	xpath.RegisterBuiltins(xctx.Funcs)
	xctx.NSMap["xsl"] = xslNS
	xctx.NSMap["xs"] = "http://www.w3.org/2001/XMLSchema"
	xctx.NSMap["fn"] = "http://www.w3.org/2005/xpath-functions"

	tc := &TransformContext{
		Stylesheet: ss,
		SourceDoc:  sourceDoc,
		ResultDoc:  dom.NewDocument(),
		XPathCtx:   xctx,
		Params:     make(map[string]xpath.Sequence),
		Keys:       make(map[string]map[string][]*dom.Node),
	}

	// Register XSLT-specific functions.
	tc.registerXSLTFunctions()

	return tc
}

func (tc *TransformContext) registerXSLTFunctions() {
	tc.XPathCtx.Funcs["current"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		if tc.CurrentNode != nil {
			return xpath.Sequence{&xpath.NodeItem{Node: tc.CurrentNode}}, nil
		}
		if ctx.Item != nil {
			return xpath.Sequence{ctx.Item}, nil
		}
		return xpath.Sequence{}, nil
	}

	tc.XPathCtx.Funcs["key"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		if len(args) < 2 {
			return xpath.Sequence{}, nil
		}
		keyName := xpath.StringValue(args[0])
		keyValue := xpath.StringValue(args[1])
		nodes := tc.lookupKey(keyName, keyValue, ctx)
		return xpath.NodesToSeq(nodes), nil
	}

	tc.XPathCtx.Funcs["current-group"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		return tc.CurrentGroup, nil
	}

	tc.XPathCtx.Funcs["current-grouping-key"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		return xpath.Sequence{xpath.StringItem(tc.CurrentGroupKey)}, nil
	}

	tc.XPathCtx.Funcs["format-number"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		if len(args) < 2 {
			return xpath.Sequence{xpath.StringItem("")}, nil
		}
		num := xpath.NumberValue(args[0])
		picture := xpath.StringValue(args[1])
		dfName := ""
		if len(args) >= 3 {
			dfName = xpath.StringValue(args[2])
		}
		df := tc.Stylesheet.DecimalFormats[dfName]
		if df == nil {
			df = defaultDecimalFormat()
		}
		result := formatNumber(num, picture, df)
		return xpath.Sequence{xpath.StringItem(result)}, nil
	}

	tc.XPathCtx.Funcs["generate-id"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		var n *dom.Node
		if len(args) > 0 && len(args[0]) > 0 && args[0][0].IsNode() {
			n = args[0][0].(*xpath.NodeItem).Node
		} else {
			n = ctx.CurrentNode()
		}
		if n == nil {
			return xpath.Sequence{xpath.StringItem("")}, nil
		}
		return xpath.Sequence{xpath.StringItem(generateID(n))}, nil
	}

	tc.XPathCtx.Funcs["unparsed-text"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		return xpath.Sequence{xpath.StringItem("")}, nil
	}

	tc.XPathCtx.Funcs["unparsed-text-available"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		return xpath.Sequence{xpath.BoolItem(false)}, nil
	}

	tc.XPathCtx.Funcs["regex-group"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		return xpath.Sequence{xpath.StringItem("")}, nil
	}

	tc.XPathCtx.Funcs["document"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		// External document loading not supported without file I/O.
		return xpath.Sequence{}, nil
	}

	tc.XPathCtx.Funcs["doc"] = tc.XPathCtx.Funcs["document"]

	tc.XPathCtx.Funcs["resolve-uri"] = func(ctx *xpath.Context, args []xpath.Sequence) (xpath.Sequence, error) {
		if len(args) > 0 {
			return args[0], nil
		}
		return xpath.Sequence{xpath.StringItem("")}, nil
	}
}

// lookupKey returns nodes that match key(name, value) in the source document.
func (tc *TransformContext) lookupKey(name, value string, ctx *xpath.Context) []*dom.Node {
	if tc.Keys[name] == nil {
		tc.buildKey(name, ctx)
	}
	return tc.Keys[name][value]
}

func (tc *TransformContext) buildKey(name string, ctx *xpath.Context) {
	var k *Key
	for _, key := range tc.Stylesheet.Keys {
		if key.Name == name {
			k = key
			break
		}
	}
	if k == nil {
		return
	}

	index := make(map[string][]*dom.Node)
	tc.Keys[name] = index

	if k.Match == nil || k.UseExpr == nil {
		return
	}

	// Walk entire document and index matching nodes.
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if k.Match.Matches(n, ctx) {
			keyCtx := ctx.Sub(&xpath.NodeItem{Node: n}, 1, 1)
			vals, err := xpath.Eval(k.UseExpr, keyCtx)
			if err == nil {
				for _, v := range vals {
					key := v.StringValue()
					index[key] = append(index[key], n)
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tc.SourceDoc.Root)
}

// SubContext returns a derived XPath context with updated node/position/size.
func (tc *TransformContext) SubContext(node *dom.Node, pos, size int) *xpath.Context {
	return tc.XPathCtx.Sub(&xpath.NodeItem{Node: node}, pos, size)
}

// WithVar returns a derived XPath context with an additional variable.
func (tc *TransformContext) WithVar(xctx *xpath.Context, name string, val xpath.Sequence) *xpath.Context {
	return xctx.WithVar(name, val)
}
