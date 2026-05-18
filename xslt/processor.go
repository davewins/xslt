package xslt

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/davewins/xslt/dom"
	"github.com/davewins/xslt/xpath"
)

// Processor applies a compiled stylesheet to source XML documents.
type Processor struct {
	Stylesheet *Stylesheet
}

// NewProcessor creates a new Processor with the given stylesheet.
func NewProcessor(ss *Stylesheet) *Processor {
	return &Processor{Stylesheet: ss}
}

// Transform applies the stylesheet to a source document and returns the result document.
func (p *Processor) Transform(sourceDoc *dom.Document, params map[string]xpath.Sequence) (*dom.Document, []string, error) {
	tc := NewTransformContext(p.Stylesheet, sourceDoc)

	// Evaluate global params and variables.
	xctx := tc.XPathCtx.Sub(&xpath.NodeItem{Node: sourceDoc.Root}, 1, 1)
	for _, param := range p.Stylesheet.Params {
		if supplied, ok := params[param.Name]; ok {
			xctx = xctx.WithVar(param.Name, supplied)
		} else if param.SelectExpr != nil {
			val, err := xpath.Eval(param.SelectExpr, xctx)
			if err != nil {
				return nil, tc.Messages, fmt.Errorf("xslt: global param %q: %w", param.Name, err)
			}
			xctx = xctx.WithVar(param.Name, val)
		}
	}
	for _, v := range p.Stylesheet.Variables {
		val, err := tc.evalVariable(v, xctx)
		if err != nil {
			return nil, tc.Messages, fmt.Errorf("xslt: global variable %q: %w", v.Name, err)
		}
		xctx = xctx.WithVar(v.Name, val)
	}
	tc.XPathCtx = xctx

	// Begin transformation at the document node.
	docCtx := tc.SubContext(sourceDoc.Root, 1, 1)
	err := p.applyTemplates(tc, docCtx, []xpath.Item{&xpath.NodeItem{Node: sourceDoc.Root}}, "", tc.ResultDoc.Root)
	if err != nil {
		return nil, tc.Messages, err
	}
	return tc.ResultDoc, tc.Messages, nil
}

// applyTemplates applies templates to a sequence of items, writing results to parent.
func (p *Processor) applyTemplates(tc *TransformContext, xctx *xpath.Context, items []xpath.Item, mode string, parent *dom.Node) error {
	size := len(items)
	for i, item := range items {
		pos := i + 1
		if item.IsNode() {
			n := item.(*xpath.NodeItem).Node
			itemCtx := xctx.Sub(item, pos, size)
			prevCurrent := tc.CurrentNode
			tc.CurrentNode = n
			tmpl := tc.Stylesheet.FindTemplate(n, mode, itemCtx)
			if tmpl == nil {
				if err := p.builtinTemplate(tc, itemCtx, n, mode, parent); err != nil {
					tc.CurrentNode = prevCurrent
					return err
				}
			} else {
				if err := p.executeTemplate(tc, itemCtx, tmpl, nil, parent); err != nil {
					tc.CurrentNode = prevCurrent
					return err
				}
			}
			tc.CurrentNode = prevCurrent
		}
	}
	return nil
}

// builtinTemplate applies the default XSLT built-in template behavior.
func (p *Processor) builtinTemplate(tc *TransformContext, xctx *xpath.Context, n *dom.Node, mode string, parent *dom.Node) error {
	switch n.Type {
	case dom.NodeDocument, dom.NodeElement:
		children := make([]xpath.Item, len(n.Children))
		for i, c := range n.Children {
			children[i] = &xpath.NodeItem{Node: c}
		}
		return p.applyTemplates(tc, xctx, children, mode, parent)
	case dom.NodeText, dom.NodeAttribute:
		addTextNode(parent, n.Value)
	}
	return nil
}

// executeTemplate executes a named template with optional parameter bindings.
func (p *Processor) executeTemplate(tc *TransformContext, xctx *xpath.Context, tmpl *Template, withParams map[string]xpath.Sequence, parent *dom.Node) error {
	ctx := xctx
	for _, param := range tmpl.Params {
		if supplied, ok := withParams[param.Name]; ok {
			ctx = ctx.WithVar(param.Name, supplied)
		} else if param.SelectExpr != nil {
			val, err := xpath.Eval(param.SelectExpr, ctx)
			if err != nil {
				return err
			}
			ctx = ctx.WithVar(param.Name, val)
		} else if hasNonParamChildren(param.Body) {
			val, err := tc.evalVariable(param, ctx)
			if err != nil {
				return err
			}
			ctx = ctx.WithVar(param.Name, val)
		}
	}
	return p.executeBody(tc, ctx, tmpl.Body, parent)
}

// executeBody processes all child nodes of a template/instruction body.
// It threads variable context changes through sequential siblings.
func (p *Processor) executeBody(tc *TransformContext, xctx *xpath.Context, bodyElem *dom.Node, parent *dom.Node) error {
	ctx := xctx
	for _, child := range bodyElem.Children {
		// Handle xsl:variable inline so subsequent siblings see the new binding.
		if child.Type == dom.NodeElement && child.NamespaceURI == xslNS && child.LocalName == "variable" {
			v, err := compileVariable(child, false)
			if err != nil {
				return err
			}
			val, err := tc.evalVariable(v, ctx)
			if err != nil {
				return fmt.Errorf("xslt: variable %q: %w", v.Name, err)
			}
			ctx = ctx.WithVar(v.Name, val)
			continue
		}
		if err := p.executeNode(tc, ctx, child, parent); err != nil {
			return err
		}
	}
	return nil
}

// executeNode processes a single node in a template body.
func (p *Processor) executeNode(tc *TransformContext, xctx *xpath.Context, node *dom.Node, parent *dom.Node) error {
	switch node.Type {
	case dom.NodeText:
		val := node.Value
		if strings.TrimSpace(val) == "" && parentIsXSL(node.Parent) {
			return nil // ignore whitespace-only text between xsl: elements
		}
		addTextNode(parent, val)
		return nil
	case dom.NodeComment, dom.NodeProcessingInstruction:
		return nil // ignored in template body
	case dom.NodeElement:
		if node.NamespaceURI == xslNS {
			return p.executeInstruction(tc, xctx, node, parent)
		}
		return p.executeLRE(tc, xctx, node, parent)
	}
	return nil
}

func parentIsXSL(n *dom.Node) bool {
	return n != nil && n.NamespaceURI == xslNS
}

// executeLRE outputs a Literal Result Element.
func (p *Processor) executeLRE(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	newEl := &dom.Node{
		Type:         dom.NodeElement,
		LocalName:    el.LocalName,
		Prefix:       el.Prefix,
		NamespaceURI: el.NamespaceURI,
		Parent:       parent,
		Document:     tc.ResultDoc,
		DocOrder:     tc.ResultDoc.NextOrder(),
	}
	// Copy non-xsl namespace bindings.
	for _, ns := range el.Namespaces {
		if ns.URI == xslNS || ns.URI == "http://www.w3.org/2001/XMLSchema" {
			continue
		}
		newEl.Namespaces = append(newEl.Namespaces, ns)
	}
	// Process attributes with AVT expansion.
	for _, attr := range el.Attributes {
		if attr.NamespaceURI == xslNS {
			continue
		}
		val, err := p.expandAVT(tc, xctx, attr.Value)
		if err != nil {
			return err
		}
		newEl.Attributes = append(newEl.Attributes, &dom.Node{
			Type:         dom.NodeAttribute,
			LocalName:    attr.LocalName,
			Prefix:       attr.Prefix,
			NamespaceURI: attr.NamespaceURI,
			Value:        val,
			Parent:       newEl,
			Document:     tc.ResultDoc,
			DocOrder:     tc.ResultDoc.NextOrder(),
		})
	}
	// Handle xsl:use-attribute-sets on LRE.
	if uses := el.MustAttrNS(xslNS, "use-attribute-sets"); uses != "" {
		if err := p.applyAttributeSets(tc, xctx, strings.Fields(uses), newEl); err != nil {
			return err
		}
	}
	parent.Children = append(parent.Children, newEl)
	return p.executeBody(tc, xctx, el, newEl)
}

// expandAVT expands an Attribute Value Template "{expr}" pattern.
func (p *Processor) expandAVT(tc *TransformContext, xctx *xpath.Context, s string) (string, error) {
	if !strings.ContainsAny(s, "{}") {
		return s, nil
	}
	var sb strings.Builder
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '{' {
			if i+1 < len(s) && s[i+1] == '{' {
				sb.WriteByte('{')
				i += 2
				continue
			}
			end := strings.Index(s[i+1:], "}")
			if end < 0 {
				return s, fmt.Errorf("xslt: unclosed '{' in AVT")
			}
			exprStr := s[i+1 : i+1+end]
			e, err := xpath.Parse(exprStr)
			if err != nil {
				return s, fmt.Errorf("xslt: AVT expression %q: %w", exprStr, err)
			}
			val, err := xpath.EvalString(e, xctx)
			if err != nil {
				return s, err
			}
			sb.WriteString(val)
			i = i + 1 + end + 1
		} else if ch == '}' && i+1 < len(s) && s[i+1] == '}' {
			sb.WriteByte('}')
			i += 2
		} else {
			sb.WriteByte(ch)
			i++
		}
	}
	return sb.String(), nil
}

// executeInstruction dispatches to the appropriate xsl: instruction handler.
func (p *Processor) executeInstruction(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	switch el.LocalName {
	case "apply-templates":
		return p.instrApplyTemplates(tc, xctx, el, parent)
	case "call-template":
		return p.instrCallTemplate(tc, xctx, el, parent)
	case "apply-imports", "next-match":
		return p.instrApplyImports(tc, xctx, el, parent)
	case "for-each":
		return p.instrForEach(tc, xctx, el, parent)
	case "for-each-group":
		return p.instrForEachGroup(tc, xctx, el, parent)
	case "if":
		return p.instrIf(tc, xctx, el, parent)
	case "choose":
		return p.instrChoose(tc, xctx, el, parent)
	case "value-of":
		return p.instrValueOf(tc, xctx, el, parent)
	case "copy-of":
		return p.instrCopyOf(tc, xctx, el, parent)
	case "copy":
		return p.instrCopy(tc, xctx, el, parent)
	case "element":
		return p.instrElement(tc, xctx, el, parent)
	case "attribute":
		return p.instrAttribute(tc, xctx, el, parent)
	case "namespace":
		return p.instrNamespace(tc, xctx, el, parent)
	case "text":
		return p.instrText(tc, xctx, el, parent)
	case "comment":
		return p.instrComment(tc, xctx, el, parent)
	case "processing-instruction":
		return p.instrPI(tc, xctx, el, parent)
	case "variable":
		// Handled in executeBody for proper context threading; should not reach here.
		return nil
	case "param":
		return nil // handled in executeTemplate
	case "with-param":
		return nil // handled inline
	case "sequence":
		return p.instrSequence(tc, xctx, el, parent)
	case "result-document":
		return p.executeBody(tc, xctx, el, parent)
	case "analyze-string":
		return p.instrAnalyzeString(tc, xctx, el, parent)
	case "message":
		return p.instrMessage(tc, xctx, el)
	case "number":
		return p.instrNumber(tc, xctx, el, parent)
	case "sort":
		return nil
	case "fallback":
		return p.executeBody(tc, xctx, el, parent)
	}
	return nil
}

func (p *Processor) instrApplyTemplates(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	selectStr := el.MustAttr("select")
	mode := el.MustAttr("mode")
	if mode == "" {
		mode = tc.Mode
	}

	var items []xpath.Item
	if selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return fmt.Errorf("xslt: apply-templates select %q: %w", selectStr, err)
		}
		seq, err := xpath.Eval(e, xctx)
		if err != nil {
			return err
		}
		items = seq
	} else {
		node := xctx.CurrentNode()
		if node != nil {
			for _, c := range node.Children {
				items = append(items, &xpath.NodeItem{Node: c})
			}
		}
	}

	// Collect sort specs.
	var sorts []sortSpec
	for _, child := range el.ChildElements() {
		if child.NamespaceURI == xslNS && child.LocalName == "sort" {
			sorts = append(sorts, makeSortSpec(child))
		}
	}
	if len(sorts) > 0 {
		items = p.sortItems(tc, xctx, items, sorts)
	}

	withParams, err := p.collectWithParams(tc, xctx, el)
	if err != nil {
		return err
	}

	prevMode := tc.Mode
	tc.Mode = mode
	size := len(items)
	for i, item := range items {
		pos := i + 1
		if item.IsNode() {
			n := item.(*xpath.NodeItem).Node
			itemCtx := xctx.Sub(item, pos, size)
			prevCurrent := tc.CurrentNode
			tc.CurrentNode = n
			tmpl := tc.Stylesheet.FindTemplate(n, mode, itemCtx)
			if tmpl == nil {
				if err := p.builtinTemplate(tc, itemCtx, n, mode, parent); err != nil {
					tc.CurrentNode = prevCurrent
					tc.Mode = prevMode
					return err
				}
			} else {
				if err := p.executeTemplate(tc, itemCtx, tmpl, withParams, parent); err != nil {
					tc.CurrentNode = prevCurrent
					tc.Mode = prevMode
					return err
				}
			}
			tc.CurrentNode = prevCurrent
		}
	}
	tc.Mode = prevMode
	return nil
}

func (p *Processor) instrCallTemplate(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	name := el.MustAttr("name")
	tmpl, ok := tc.Stylesheet.NamedTemplates[name]
	if !ok {
		return fmt.Errorf("xslt: no template named %q", name)
	}
	withParams, err := p.collectWithParams(tc, xctx, el)
	if err != nil {
		return err
	}
	return p.executeTemplate(tc, xctx, tmpl, withParams, parent)
}

func (p *Processor) instrApplyImports(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	node := xctx.CurrentNode()
	if node != nil {
		return p.builtinTemplate(tc, xctx, node, tc.Mode, parent)
	}
	return nil
}

func (p *Processor) instrForEach(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	selectStr := el.MustAttr("select")
	if selectStr == "" {
		return fmt.Errorf("xslt: xsl:for-each requires select attribute")
	}
	e, err := xpath.Parse(selectStr)
	if err != nil {
		return fmt.Errorf("xslt: for-each select %q: %w", selectStr, err)
	}
	seq, err := xpath.Eval(e, xctx)
	if err != nil {
		return err
	}

	var sorts []sortSpec
	for _, child := range el.ChildElements() {
		if child.NamespaceURI == xslNS && child.LocalName == "sort" {
			sorts = append(sorts, makeSortSpec(child))
		}
	}
	if len(sorts) > 0 {
		seq = p.sortItems(tc, xctx, seq, sorts)
	}

	size := len(seq)
	for i, item := range seq {
		iterCtx := xctx.Sub(item, i+1, size)
		prevCurrent := tc.CurrentNode
		if item.IsNode() {
			tc.CurrentNode = item.(*xpath.NodeItem).Node
		}
		if err := p.executeBody(tc, iterCtx, el, parent); err != nil {
			tc.CurrentNode = prevCurrent
			return err
		}
		tc.CurrentNode = prevCurrent
	}
	return nil
}

func (p *Processor) instrForEachGroup(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	selectStr := el.MustAttr("select")
	groupBy := el.MustAttr("group-by")
	groupAdjacent := el.MustAttr("group-adjacent")
	groupStartingWith := el.MustAttr("group-starting-with")
	groupEndingWith := el.MustAttr("group-ending-with")

	e, err := xpath.Parse(selectStr)
	if err != nil {
		return fmt.Errorf("xslt: for-each-group select %q: %w", selectStr, err)
	}
	seq, err := xpath.Eval(e, xctx)
	if err != nil {
		return err
	}

	var sorts []sortSpec
	for _, child := range el.ChildElements() {
		if child.NamespaceURI == xslNS && child.LocalName == "sort" {
			sorts = append(sorts, makeSortSpec(child))
		}
	}

	type group struct {
		key   string
		items []xpath.Item
	}
	var groups []group

	switch {
	case groupBy != "":
		keyExpr, err := xpath.Parse(groupBy)
		if err != nil {
			return fmt.Errorf("xslt: for-each-group group-by %q: %w", groupBy, err)
		}
		keyMap := make(map[string][]xpath.Item)
		var keyOrder []string
		keySet := make(map[string]bool)
		for _, item := range seq {
			itemCtx := xctx.Sub(item, 1, len(seq))
			keySeq, err := xpath.Eval(keyExpr, itemCtx)
			if err != nil {
				return err
			}
			key := xpath.StringValue(keySeq)
			if !keySet[key] {
				keySet[key] = true
				keyOrder = append(keyOrder, key)
			}
			keyMap[key] = append(keyMap[key], item)
		}
		for _, k := range keyOrder {
			groups = append(groups, group{k, keyMap[k]})
		}

	case groupAdjacent != "":
		keyExpr, err := xpath.Parse(groupAdjacent)
		if err != nil {
			return fmt.Errorf("xslt: for-each-group group-adjacent %q: %w", groupAdjacent, err)
		}
		prevKey := ""
		for _, item := range seq {
			itemCtx := xctx.Sub(item, 1, len(seq))
			keySeq, err := xpath.Eval(keyExpr, itemCtx)
			if err != nil {
				return err
			}
			key := xpath.StringValue(keySeq)
			if len(groups) == 0 || key != prevKey {
				groups = append(groups, group{key, nil})
				prevKey = key
			}
			groups[len(groups)-1].items = append(groups[len(groups)-1].items, item)
		}

	case groupStartingWith != "":
		pat, err := xpath.CompilePattern(groupStartingWith)
		if err != nil {
			return fmt.Errorf("xslt: for-each-group group-starting-with %q: %w", groupStartingWith, err)
		}
		for _, item := range seq {
			startsGroup := item.IsNode() && pat.Matches(item.(*xpath.NodeItem).Node, xctx)
			if len(groups) == 0 || startsGroup {
				groups = append(groups, group{strconv.Itoa(len(groups)), nil})
			}
			if len(groups) > 0 {
				groups[len(groups)-1].items = append(groups[len(groups)-1].items, item)
			}
		}

	case groupEndingWith != "":
		pat, err := xpath.CompilePattern(groupEndingWith)
		if err != nil {
			return fmt.Errorf("xslt: for-each-group group-ending-with %q: %w", groupEndingWith, err)
		}
		groups = append(groups, group{"0", nil})
		for _, item := range seq {
			if len(groups) > 0 {
				groups[len(groups)-1].items = append(groups[len(groups)-1].items, item)
			}
			if item.IsNode() && pat.Matches(item.(*xpath.NodeItem).Node, xctx) {
				groups = append(groups, group{strconv.Itoa(len(groups)), nil})
			}
		}
		// Remove trailing empty group.
		for len(groups) > 0 && len(groups[len(groups)-1].items) == 0 {
			groups = groups[:len(groups)-1]
		}

	default:
		return fmt.Errorf("xslt: for-each-group requires a grouping attribute")
	}

	// Sort groups if sort specs are present.
	if len(sorts) > 0 {
		sort.SliceStable(groups, func(i, j int) bool {
			ki := groupSortKey(tc, xctx, groups[i].items, sorts)
			kj := groupSortKey(tc, xctx, groups[j].items, sorts)
			if sorts[0].order == "descending" {
				return ki > kj
			}
			return ki < kj
		})
	}

	// Execute body for each group.
	size := len(groups)
	for i, g := range groups {
		prevGroup := tc.CurrentGroup
		prevGroupKey := tc.CurrentGroupKey
		tc.CurrentGroup = g.items
		tc.CurrentGroupKey = g.key
		var groupCtx *xpath.Context
		if len(g.items) > 0 {
			groupCtx = xctx.Sub(g.items[0], i+1, size)
		} else {
			groupCtx = xctx.Sub(nil, i+1, size)
		}
		if err := p.executeBody(tc, groupCtx, el, parent); err != nil {
			tc.CurrentGroup = prevGroup
			tc.CurrentGroupKey = prevGroupKey
			return err
		}
		tc.CurrentGroup = prevGroup
		tc.CurrentGroupKey = prevGroupKey
	}
	return nil
}

func groupSortKey(tc *TransformContext, xctx *xpath.Context, items []xpath.Item, sorts []sortSpec) string {
	if len(items) == 0 || sorts[0].selectExpr == nil {
		return ""
	}
	ctx := xctx.Sub(items[0], 1, len(items))
	seq, err := xpath.Eval(sorts[0].selectExpr, ctx)
	if err != nil {
		return ""
	}
	return xpath.StringValue(seq)
}

func (p *Processor) instrIf(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	testStr := el.MustAttr("test")
	e, err := xpath.Parse(testStr)
	if err != nil {
		return fmt.Errorf("xslt: if test %q: %w", testStr, err)
	}
	result, err := xpath.EvalBool(e, xctx)
	if err != nil {
		return err
	}
	if result {
		return p.executeBody(tc, xctx, el, parent)
	}
	return nil
}

func (p *Processor) instrChoose(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	for _, child := range el.ChildElements() {
		if child.NamespaceURI != xslNS {
			continue
		}
		switch child.LocalName {
		case "when":
			e, err := xpath.Parse(child.MustAttr("test"))
			if err != nil {
				return fmt.Errorf("xslt: when test: %w", err)
			}
			result, err := xpath.EvalBool(e, xctx)
			if err != nil {
				return err
			}
			if result {
				return p.executeBody(tc, xctx, child, parent)
			}
		case "otherwise":
			return p.executeBody(tc, xctx, child, parent)
		}
	}
	return nil
}

func (p *Processor) instrValueOf(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	selectStr := el.MustAttr("select")
	separator := el.MustAttr("separator")
	disableEsc := strings.EqualFold(el.MustAttr("disable-output-escaping"), "yes")

	var text string
	if selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return fmt.Errorf("xslt: value-of select %q: %w", selectStr, err)
		}
		seq, err := xpath.Eval(e, xctx)
		if err != nil {
			return err
		}
		if separator == "" {
			// XPath 2.0 default: space-separated.
			parts := make([]string, len(seq))
			for i, item := range seq {
				parts[i] = item.StringValue()
			}
			text = strings.Join(parts, " ")
		} else {
			parts := make([]string, len(seq))
			for i, item := range seq {
				parts[i] = item.StringValue()
			}
			text = strings.Join(parts, separator)
		}
	} else if xctx.Item != nil {
		text = xctx.Item.StringValue()
	}

	if disableEsc {
		addRawTextNode(parent, text, tc.ResultDoc)
	} else {
		addTextNode(parent, text)
	}
	return nil
}

func (p *Processor) instrCopyOf(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	selectStr := el.MustAttr("select")
	if selectStr == "" {
		return fmt.Errorf("xslt: xsl:copy-of requires select attribute")
	}
	e, err := xpath.Parse(selectStr)
	if err != nil {
		return fmt.Errorf("xslt: copy-of select %q: %w", selectStr, err)
	}
	seq, err := xpath.Eval(e, xctx)
	if err != nil {
		return err
	}
	for _, item := range seq {
		if item.IsNode() {
			copyNode(item.(*xpath.NodeItem).Node, parent, tc.ResultDoc)
		} else {
			addTextNode(parent, item.StringValue())
		}
	}
	return nil
}

func (p *Processor) instrCopy(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	src := xctx.CurrentNode()
	if src == nil {
		return nil
	}
	switch src.Type {
	case dom.NodeElement:
		newEl := &dom.Node{
			Type:         dom.NodeElement,
			LocalName:    src.LocalName,
			Prefix:       src.Prefix,
			NamespaceURI: src.NamespaceURI,
			Namespaces:   src.Namespaces,
			Parent:       parent,
			Document:     tc.ResultDoc,
			DocOrder:     tc.ResultDoc.NextOrder(),
		}
		parent.Children = append(parent.Children, newEl)
		return p.executeBody(tc, xctx, el, newEl)
	case dom.NodeDocument:
		return p.executeBody(tc, xctx, el, parent)
	default:
		copyNode(src, parent, tc.ResultDoc)
		return p.executeBody(tc, xctx, el, parent)
	}
}

func (p *Processor) instrElement(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	nameStr, err := p.expandAVT(tc, xctx, el.MustAttr("name"))
	if err != nil {
		return err
	}
	nsStr := el.MustAttr("namespace")
	if nsStr != "" {
		if nsStr, err = p.expandAVT(tc, xctx, nsStr); err != nil {
			return err
		}
	}
	prefix, local := splitQName(nameStr)
	nsURI := nsStr
	if nsURI == "" && prefix != "" {
		nsURI = xctx.ResolveNamespace(prefix)
	}
	newEl := &dom.Node{
		Type:         dom.NodeElement,
		LocalName:    local,
		Prefix:       prefix,
		NamespaceURI: nsURI,
		Parent:       parent,
		Document:     tc.ResultDoc,
		DocOrder:     tc.ResultDoc.NextOrder(),
	}
	parent.Children = append(parent.Children, newEl)
	if uses := el.MustAttr("use-attribute-sets"); uses != "" {
		if err := p.applyAttributeSets(tc, xctx, strings.Fields(uses), newEl); err != nil {
			return err
		}
	}
	return p.executeBody(tc, xctx, el, newEl)
}

func (p *Processor) instrAttribute(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	if parent.Type != dom.NodeElement {
		return nil
	}
	nameStr, err := p.expandAVT(tc, xctx, el.MustAttr("name"))
	if err != nil {
		return err
	}
	nsStr := el.MustAttr("namespace")
	if nsStr != "" {
		if nsStr, err = p.expandAVT(tc, xctx, nsStr); err != nil {
			return err
		}
	}
	prefix, local := splitQName(nameStr)
	nsURI := nsStr
	if nsURI == "" && prefix != "" {
		nsURI = xctx.ResolveNamespace(prefix)
	}

	var val string
	if selectStr := el.MustAttr("select"); selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return err
		}
		seq, err := xpath.Eval(e, xctx)
		if err != nil {
			return err
		}
		sep := el.MustAttr("separator")
		if sep == "" {
			sep = " "
		}
		parts := make([]string, len(seq))
		for i, item := range seq {
			parts[i] = item.StringValue()
		}
		val = strings.Join(parts, sep)
	} else {
		// Evaluate content as a temporary tree.
		var sb strings.Builder
		for _, c := range el.Children {
			if c.Type == dom.NodeText {
				sb.WriteString(c.Value)
			}
		}
		val = sb.String()
		if val == "" {
			// Try executing body instructions.
			temp := &dom.Node{Type: dom.NodeDocument, Document: tc.ResultDoc}
			if err := p.executeBody(tc, xctx, el, temp); err != nil {
				return err
			}
			val = temp.StringValue()
		}
	}
	// Remove existing attribute with same name.
	for i, a := range parent.Attributes {
		if a.LocalName == local && a.NamespaceURI == nsURI {
			parent.Attributes = append(parent.Attributes[:i], parent.Attributes[i+1:]...)
			break
		}
	}
	parent.Attributes = append(parent.Attributes, &dom.Node{
		Type:         dom.NodeAttribute,
		LocalName:    local,
		Prefix:       prefix,
		NamespaceURI: nsURI,
		Value:        val,
		Parent:       parent,
		Document:     tc.ResultDoc,
		DocOrder:     tc.ResultDoc.NextOrder(),
	})
	return nil
}

func (p *Processor) instrNamespace(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	nameStr, err := p.expandAVT(tc, xctx, el.MustAttr("name"))
	if err != nil {
		return err
	}
	var uri string
	if selectStr := el.MustAttr("select"); selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return err
		}
		uri, err = xpath.EvalString(e, xctx)
		if err != nil {
			return err
		}
	} else {
		var sb strings.Builder
		for _, c := range el.Children {
			if c.Type == dom.NodeText {
				sb.WriteString(c.Value)
			}
		}
		uri = sb.String()
	}
	if parent.Type == dom.NodeElement {
		parent.Namespaces = append(parent.Namespaces, dom.Namespace{Prefix: nameStr, URI: uri})
	}
	return nil
}

func (p *Processor) instrText(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	var sb strings.Builder
	for _, c := range el.Children {
		if c.Type == dom.NodeText {
			sb.WriteString(c.Value)
		}
	}
	disableEsc := strings.EqualFold(el.MustAttr("disable-output-escaping"), "yes")
	if disableEsc {
		addRawTextNode(parent, sb.String(), tc.ResultDoc)
	} else {
		addTextNode(parent, sb.String())
	}
	return nil
}

func (p *Processor) instrComment(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	var val string
	if selectStr := el.MustAttr("select"); selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return err
		}
		val, err = xpath.EvalString(e, xctx)
		if err != nil {
			return err
		}
	} else {
		temp := &dom.Node{Type: dom.NodeDocument, Document: tc.ResultDoc}
		if err := p.executeBody(tc, xctx, el, temp); err != nil {
			return err
		}
		val = temp.StringValue()
	}
	parent.Children = append(parent.Children, &dom.Node{
		Type:     dom.NodeComment,
		Value:    val,
		Parent:   parent,
		Document: tc.ResultDoc,
		DocOrder: tc.ResultDoc.NextOrder(),
	})
	return nil
}

func (p *Processor) instrPI(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	nameStr, err := p.expandAVT(tc, xctx, el.MustAttr("name"))
	if err != nil {
		return err
	}
	var val string
	if selectStr := el.MustAttr("select"); selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return err
		}
		val, err = xpath.EvalString(e, xctx)
		if err != nil {
			return err
		}
	} else {
		temp := &dom.Node{Type: dom.NodeDocument, Document: tc.ResultDoc}
		if err := p.executeBody(tc, xctx, el, temp); err != nil {
			return err
		}
		val = temp.StringValue()
	}
	parent.Children = append(parent.Children, &dom.Node{
		Type:      dom.NodeProcessingInstruction,
		LocalName: nameStr,
		Value:     val,
		Parent:    parent,
		Document:  tc.ResultDoc,
		DocOrder:  tc.ResultDoc.NextOrder(),
	})
	return nil
}

func (p *Processor) instrSequence(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	if selectStr := el.MustAttr("select"); selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return fmt.Errorf("xslt: sequence select %q: %w", selectStr, err)
		}
		seq, err := xpath.Eval(e, xctx)
		if err != nil {
			return err
		}
		for _, item := range seq {
			if item.IsNode() {
				copyNode(item.(*xpath.NodeItem).Node, parent, tc.ResultDoc)
			} else {
				addTextNode(parent, item.StringValue())
			}
		}
		return nil
	}
	return p.executeBody(tc, xctx, el, parent)
}

func (p *Processor) instrAnalyzeString(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	var input string
	if selectStr := el.MustAttr("select"); selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return err
		}
		input, err = xpath.EvalString(e, xctx)
		if err != nil {
			return err
		}
	} else if xctx.Item != nil {
		input = xctx.Item.StringValue()
	}

	regex := el.MustAttr("regex")
	flags := el.MustAttr("flags")

	re, err := compileRegex(regex, flags)
	if err != nil {
		return fmt.Errorf("xslt: analyze-string regex %q: %w", regex, err)
	}

	var matchingNode, nonMatchingNode *dom.Node
	for _, child := range el.ChildElements() {
		if child.NamespaceURI == xslNS {
			switch child.LocalName {
			case "matching-substring":
				matchingNode = child
			case "non-matching-substring":
				nonMatchingNode = child
			}
		}
	}

	matches := re.FindAllStringIndex(input, -1)
	pos := 0
	for _, m := range matches {
		if m[0] > pos && nonMatchingNode != nil {
			sub := input[pos:m[0]]
			subCtx := xctx.Sub(xpath.StringItem(sub), 1, 1)
			if err := p.executeBody(tc, subCtx, nonMatchingNode, parent); err != nil {
				return err
			}
		}
		if matchingNode != nil {
			sub := input[m[0]:m[1]]
			subCtx := xctx.Sub(xpath.StringItem(sub), 1, 1)
			if err := p.executeBody(tc, subCtx, matchingNode, parent); err != nil {
				return err
			}
		}
		pos = m[1]
	}
	if pos < len(input) && nonMatchingNode != nil {
		sub := input[pos:]
		subCtx := xctx.Sub(xpath.StringItem(sub), 1, 1)
		if err := p.executeBody(tc, subCtx, nonMatchingNode, parent); err != nil {
			return err
		}
	}
	return nil
}

func (p *Processor) instrMessage(tc *TransformContext, xctx *xpath.Context, el *dom.Node) error {
	var sb strings.Builder
	if selectStr := el.MustAttr("select"); selectStr != "" {
		e, err := xpath.Parse(selectStr)
		if err != nil {
			return err
		}
		seq, err := xpath.Eval(e, xctx)
		if err != nil {
			return err
		}
		sb.WriteString(xpath.StringValue(seq))
	}
	temp := &dom.Node{Type: dom.NodeDocument, Document: tc.ResultDoc}
	if err := p.executeBody(tc, xctx, el, temp); err != nil {
		return err
	}
	sb.WriteString(temp.StringValue())
	msg := strings.TrimSpace(sb.String())
	if msg == "" {
		msg = el.MustAttr("select")
	}
	tc.Messages = append(tc.Messages, msg)
	if strings.EqualFold(el.MustAttr("terminate"), "yes") {
		return fmt.Errorf("xslt: xsl:message terminate: %s", msg)
	}
	return nil
}

func (p *Processor) instrNumber(tc *TransformContext, xctx *xpath.Context, el *dom.Node, parent *dom.Node) error {
	format := el.MustAttr("format")
	if format == "" {
		format = "1"
	}
	var num float64
	if valueStr := el.MustAttr("value"); valueStr != "" {
		e, err := xpath.Parse(valueStr)
		if err != nil {
			return err
		}
		seq, err := xpath.Eval(e, xctx)
		if err != nil {
			return err
		}
		num = xpath.NumberValue(seq)
	} else {
		num = float64(calcNodePosition(xctx, el))
	}
	addTextNode(parent, formatNumberAsXSLT(num, format))
	return nil
}

func calcNodePosition(xctx *xpath.Context, el *dom.Node) int {
	n := xctx.CurrentNode()
	if n == nil || n.Parent == nil {
		return 1
	}
	pos := 0
	for _, s := range n.Parent.Children {
		if s.Type == n.Type && s.LocalName == n.LocalName {
			pos++
			if s == n {
				return pos
			}
		}
	}
	return 1
}

func formatNumberAsXSLT(num float64, format string) string {
	switch format {
	case "i":
		return strings.ToLower(toRoman(int(num)))
	case "I":
		return toRoman(int(num))
	case "a":
		return toAlpha(int(num))
	case "A":
		return strings.ToUpper(toAlpha(int(num)))
	default:
		return strconv.Itoa(int(num))
	}
}

func toRoman(n int) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var sb strings.Builder
	for i, val := range vals {
		for n >= val {
			sb.WriteString(syms[i])
			n -= val
		}
	}
	return sb.String()
}

func toAlpha(n int) string {
	if n <= 0 {
		return ""
	}
	result := ""
	for n > 0 {
		n--
		result = string(rune('a'+n%26)) + result
		n /= 26
	}
	return result
}

// collectWithParams gathers xsl:with-param children into a map.
func (p *Processor) collectWithParams(tc *TransformContext, xctx *xpath.Context, el *dom.Node) (map[string]xpath.Sequence, error) {
	params := make(map[string]xpath.Sequence)
	for _, child := range el.ChildElements() {
		if child.NamespaceURI != xslNS || child.LocalName != "with-param" {
			continue
		}
		name := child.MustAttr("name")
		if selectStr := child.MustAttr("select"); selectStr != "" {
			e, err := xpath.Parse(selectStr)
			if err != nil {
				return nil, fmt.Errorf("xslt: with-param select %q: %w", selectStr, err)
			}
			val, err := xpath.Eval(e, xctx)
			if err != nil {
				return nil, err
			}
			params[name] = val
		} else {
			temp := &dom.Node{Type: dom.NodeDocument, Document: tc.ResultDoc}
			if err := p.executeBody(tc, xctx, child, temp); err != nil {
				return nil, err
			}
			params[name] = xpath.Sequence{xpath.StringItem(temp.StringValue())}
		}
	}
	return params, nil
}

// applyAttributeSets applies named attribute sets to an element.
func (p *Processor) applyAttributeSets(tc *TransformContext, xctx *xpath.Context, names []string, el *dom.Node) error {
	for _, name := range names {
		as, ok := tc.Stylesheet.AttributeSets[name]
		if !ok {
			continue
		}
		if err := p.applyAttributeSets(tc, xctx, as.UseAttributeSets, el); err != nil {
			return err
		}
		for _, child := range as.Body.ChildElements() {
			if child.NamespaceURI == xslNS && child.LocalName == "attribute" {
				if err := p.instrAttribute(tc, xctx, child, el); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type sortSpec struct {
	selectStr  string
	selectExpr xpath.Expr
	order      string
	dataType   string
	caseOrder  string
	lang       string
	stable     bool
}

func makeSortSpec(el *dom.Node) sortSpec {
	s := sortSpec{
		selectStr: el.MustAttr("select"),
		order:     el.MustAttr("order"),
		dataType:  el.MustAttr("data-type"),
		caseOrder: el.MustAttr("case-order"),
		lang:      el.MustAttr("lang"),
		stable:    !strings.EqualFold(el.MustAttr("stable"), "no"),
	}
	if s.order == "" {
		s.order = "ascending"
	}
	if s.selectStr != "" {
		if e, err := xpath.Parse(s.selectStr); err == nil {
			s.selectExpr = e
		}
	}
	return s
}

func (p *Processor) sortItems(tc *TransformContext, xctx *xpath.Context, items []xpath.Item, sorts []sortSpec) []xpath.Item {
	type pair struct {
		item xpath.Item
		keys []string
	}
	pairs := make([]pair, len(items))
	for i, item := range items {
		keys := make([]string, len(sorts))
		for j, s := range sorts {
			itemCtx := xctx.Sub(item, i+1, len(items))
			if s.selectExpr != nil {
				seq, err := xpath.Eval(s.selectExpr, itemCtx)
				if err == nil {
					keys[j] = xpath.StringValue(seq)
					continue
				}
			}
			keys[j] = item.StringValue()
		}
		pairs[i] = pair{item, keys}
	}
	sort.SliceStable(pairs, func(a, b int) bool {
		for k, s := range sorts {
			ak, bk := pairs[a].keys[k], pairs[b].keys[k]
			if s.dataType == "number" {
				af, _ := strconv.ParseFloat(ak, 64)
				bf, _ := strconv.ParseFloat(bk, 64)
				if af != bf {
					if s.order == "descending" {
						return af > bf
					}
					return af < bf
				}
			} else {
				if ak != bk {
					if s.order == "descending" {
						return ak > bk
					}
					return ak < bk
				}
			}
		}
		return false
	})
	out := make([]xpath.Item, len(items))
	for i, pr := range pairs {
		out[i] = pr.item
	}
	return out
}

// Node tree helpers.

func addTextNode(parent *dom.Node, text string) {
	if text == "" {
		return
	}
	// Coalesce adjacent text nodes.
	if n := len(parent.Children); n > 0 {
		last := parent.Children[n-1]
		if last.Type == dom.NodeText && !isRaw(last) {
			last.Value += text
			return
		}
	}
	parent.Children = append(parent.Children, &dom.Node{
		Type:     dom.NodeText,
		Value:    text,
		Parent:   parent,
		Document: parent.Document,
	})
}

func isRaw(n *dom.Node) bool {
	for _, a := range n.Attributes {
		if a.LocalName == "raw" {
			return true
		}
	}
	return false
}

func addRawTextNode(parent *dom.Node, text string, doc *dom.Document) {
	n := &dom.Node{
		Type:     dom.NodeText,
		Value:    text,
		Parent:   parent,
		Document: doc,
	}
	n.Attributes = []*dom.Node{{Type: dom.NodeAttribute, LocalName: "raw", Value: "yes"}}
	parent.Children = append(parent.Children, n)
}

func copyNode(src *dom.Node, parent *dom.Node, doc *dom.Document) {
	switch src.Type {
	case dom.NodeElement:
		newEl := &dom.Node{
			Type:         dom.NodeElement,
			LocalName:    src.LocalName,
			Prefix:       src.Prefix,
			NamespaceURI: src.NamespaceURI,
			Namespaces:   append([]dom.Namespace(nil), src.Namespaces...),
			Parent:       parent,
			Document:     doc,
			DocOrder:     doc.NextOrder(),
		}
		for _, a := range src.Attributes {
			newEl.Attributes = append(newEl.Attributes, &dom.Node{
				Type: dom.NodeAttribute, LocalName: a.LocalName,
				Prefix: a.Prefix, NamespaceURI: a.NamespaceURI,
				Value: a.Value, Parent: newEl, Document: doc,
			})
		}
		parent.Children = append(parent.Children, newEl)
		for _, c := range src.Children {
			copyNode(c, newEl, doc)
		}
	case dom.NodeText:
		addTextNode(parent, src.Value)
	case dom.NodeComment:
		parent.Children = append(parent.Children, &dom.Node{
			Type: dom.NodeComment, Value: src.Value,
			Parent: parent, Document: doc, DocOrder: doc.NextOrder(),
		})
	case dom.NodeProcessingInstruction:
		parent.Children = append(parent.Children, &dom.Node{
			Type: dom.NodeProcessingInstruction, LocalName: src.LocalName,
			Value: src.Value, Parent: parent, Document: doc, DocOrder: doc.NextOrder(),
		})
	}
}

func splitQName(qname string) (prefix, local string) {
	if idx := strings.Index(qname, ":"); idx >= 0 {
		return qname[:idx], qname[idx+1:]
	}
	return "", qname
}
