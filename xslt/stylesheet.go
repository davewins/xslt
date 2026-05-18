package xslt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/davewins/xslt/dom"
	"github.com/davewins/xslt/xpath"
)

const xslNS = "http://www.w3.org/1999/XSL/Transform"

// Stylesheet represents a compiled XSLT 2.0 stylesheet.
type Stylesheet struct {
	Version         string
	OutputMethod    string
	OutputEncoding  string
	OutputIndent    bool
	OmitXMLDecl     bool
	DoctypePublic   string
	DoctypeSystem   string
	MediaType       string

	Templates       []*Template
	NamedTemplates  map[string]*Template
	Variables       []*Variable
	Params          []*Variable
	Keys            []*Key
	AttributeSets   map[string]*AttributeSet
	DecimalFormats  map[string]*DecimalFormat
	Functions       map[string]*Function
	NSAliases       map[string]string // stylesheet-prefix → result-prefix
	StripSpaceElems map[string]bool   // elements to strip whitespace from
	PreserveSpaceElems map[string]bool

	// Import precedence — templates from imported stylesheets have lower precedence.
	ImportLevel int
}

// Template represents an xsl:template declaration.
type Template struct {
	Match    *xpath.Pattern
	Name     string
	Mode     string
	Priority float64
	HasPriorityAttr bool
	Params   []*Variable
	Body     *dom.Node // the xsl:template element itself
	ImportLevel int
}

// Variable represents xsl:variable or xsl:param.
type Variable struct {
	Name   string
	Select string
	SelectExpr xpath.Expr
	Body   *dom.Node
	As     string
	Required bool
	Tunnel   bool
}

// Key represents an xsl:key declaration.
type Key struct {
	Name  string
	Match *xpath.Pattern
	Use   string
	UseExpr xpath.Expr
	index map[string][]*dom.Node // built lazily
}

// AttributeSet represents xsl:attribute-set.
type AttributeSet struct {
	Name            string
	UseAttributeSets []string
	Body            *dom.Node
}

// DecimalFormat represents xsl:decimal-format.
type DecimalFormat struct {
	Name            string
	DecimalSeparator rune
	GroupingSeparator rune
	Infinity        string
	MinusSign       rune
	NaN             string
	Percent         rune
	PerMille        rune
	ZeroDigit       rune
	Digit           rune
	PatternSeparator rune
}

// Function represents xsl:function.
type Function struct {
	Name   string
	Params []*Variable
	Body   *dom.Node
	As     string
}

// NewStylesheet creates an empty stylesheet with defaults.
func NewStylesheet() *Stylesheet {
	return &Stylesheet{
		Version:        "2.0",
		OutputMethod:   "xml",
		OutputEncoding: "UTF-8",
		NamedTemplates: make(map[string]*Template),
		AttributeSets:  make(map[string]*AttributeSet),
		DecimalFormats: make(map[string]*DecimalFormat),
		Functions:      make(map[string]*Function),
		NSAliases:      make(map[string]string),
		StripSpaceElems: make(map[string]bool),
		PreserveSpaceElems: make(map[string]bool),
	}
}

// Compile parses and compiles an XSLT 2.0 stylesheet from a DOM document.
func Compile(doc *dom.Document) (*Stylesheet, error) {
	ss := NewStylesheet()
	root := doc.DocumentElement()
	if root == nil {
		return nil, fmt.Errorf("xslt: empty stylesheet document")
	}
	if root.NamespaceURI != xslNS {
		return nil, fmt.Errorf("xslt: root element must be in XSL namespace, got %q", root.NamespaceURI)
	}
	if root.LocalName != "stylesheet" && root.LocalName != "transform" {
		return nil, fmt.Errorf("xslt: root element must be xsl:stylesheet or xsl:transform, got %q", root.LocalName)
	}
	ss.Version = root.MustAttr("version")

	// Strip whitespace-only text nodes from stylesheet (XSLT spec requirement).
	stripStylesheetWhitespace(root)

	if err := ss.processTopLevel(root, 0); err != nil {
		return nil, err
	}
	return ss, nil
}

// stripStylesheetWhitespace removes whitespace-only text nodes from the stylesheet DOM,
// except inside xsl:text elements where whitespace is significant.
func stripStylesheetWhitespace(n *dom.Node) {
	if n.NamespaceURI == xslNS && n.LocalName == "text" {
		return // preserve content of xsl:text
	}
	var kept []*dom.Node
	for _, c := range n.Children {
		if c.Type == dom.NodeText && strings.TrimSpace(c.Value) == "" {
			continue
		}
		kept = append(kept, c)
	}
	n.Children = kept
	for _, c := range n.Children {
		if c.Type == dom.NodeElement {
			stripStylesheetWhitespace(c)
		}
	}
}

func (ss *Stylesheet) processTopLevel(root *dom.Node, importLevel int) error {
	for _, child := range root.ChildElements() {
		if child.NamespaceURI != xslNS {
			continue
		}
		if err := ss.processTopLevelElement(child, importLevel); err != nil {
			return err
		}
	}
	return nil
}

func (ss *Stylesheet) processTopLevelElement(el *dom.Node, importLevel int) error {
	switch el.LocalName {
	case "import", "include":
		// xsl:import/include: load and merge the referenced stylesheet.
		// For now, we skip external loading (no file I/O).
		// In a full implementation, resolve href and load.
		return nil

	case "template":
		tmpl, err := ss.compileTemplate(el, importLevel)
		if err != nil {
			return err
		}
		ss.Templates = append(ss.Templates, tmpl)
		if tmpl.Name != "" {
			ss.NamedTemplates[tmpl.Name] = tmpl
		}

	case "variable":
		v, err := compileVariable(el, false)
		if err != nil {
			return err
		}
		ss.Variables = append(ss.Variables, v)

	case "param":
		v, err := compileVariable(el, true)
		if err != nil {
			return err
		}
		ss.Params = append(ss.Params, v)

	case "key":
		k, err := compileKey(el)
		if err != nil {
			return err
		}
		ss.Keys = append(ss.Keys, k)

	case "attribute-set":
		as := &AttributeSet{
			Name: el.MustAttr("name"),
			Body: el,
		}
		if uses := el.MustAttr("use-attribute-sets"); uses != "" {
			as.UseAttributeSets = strings.Fields(uses)
		}
		ss.AttributeSets[as.Name] = as

	case "output":
		ss.processOutput(el)

	case "strip-space":
		for _, name := range strings.Fields(el.MustAttr("elements")) {
			ss.StripSpaceElems[name] = true
		}

	case "preserve-space":
		for _, name := range strings.Fields(el.MustAttr("elements")) {
			ss.PreserveSpaceElems[name] = true
		}

	case "decimal-format":
		df := defaultDecimalFormat()
		df.Name = el.MustAttr("name")
		if v := el.MustAttr("decimal-separator"); v != "" {
			df.DecimalSeparator = []rune(v)[0]
		}
		if v := el.MustAttr("grouping-separator"); v != "" {
			df.GroupingSeparator = []rune(v)[0]
		}
		if v := el.MustAttr("infinity"); v != "" {
			df.Infinity = v
		}
		if v := el.MustAttr("NaN"); v != "" {
			df.NaN = v
		}
		ss.DecimalFormats[df.Name] = df

	case "namespace-alias":
		sp := el.MustAttr("stylesheet-prefix")
		rp := el.MustAttr("result-prefix")
		ss.NSAliases[sp] = rp

	case "function":
		fn, err := ss.compileFunction(el)
		if err != nil {
			return err
		}
		ss.Functions[fn.Name] = fn
	}
	return nil
}

func (ss *Stylesheet) compileTemplate(el *dom.Node, importLevel int) (*Template, error) {
	tmpl := &Template{
		Name:        el.MustAttr("name"),
		Mode:        el.MustAttr("mode"),
		ImportLevel: importLevel,
		Body:        el,
	}

	matchStr := el.MustAttr("match")
	if matchStr != "" {
		pat, err := xpath.CompilePattern(matchStr)
		if err != nil {
			return nil, fmt.Errorf("xslt: template match %q: %w", matchStr, err)
		}
		tmpl.Match = pat
		tmpl.Priority = pat.Priority()
	}

	if prioStr := el.MustAttr("priority"); prioStr != "" {
		prio, err := strconv.ParseFloat(prioStr, 64)
		if err != nil {
			return nil, fmt.Errorf("xslt: template priority %q: %w", prioStr, err)
		}
		tmpl.Priority = prio
		tmpl.HasPriorityAttr = true
	}

	// Compile template params.
	for _, child := range el.ChildElements() {
		if child.NamespaceURI == xslNS && child.LocalName == "param" {
			v, err := compileVariable(child, true)
			if err != nil {
				return nil, err
			}
			tmpl.Params = append(tmpl.Params, v)
		}
	}

	return tmpl, nil
}

func (ss *Stylesheet) processOutput(el *dom.Node) {
	if v := el.MustAttr("method"); v != "" {
		ss.OutputMethod = v
	}
	if v := el.MustAttr("encoding"); v != "" {
		ss.OutputEncoding = v
	}
	if v := el.MustAttr("indent"); v != "" {
		ss.OutputIndent = strings.EqualFold(v, "yes")
	}
	if v := el.MustAttr("omit-xml-declaration"); v != "" {
		ss.OmitXMLDecl = strings.EqualFold(v, "yes")
	}
	if v := el.MustAttr("doctype-public"); v != "" {
		ss.DoctypePublic = v
	}
	if v := el.MustAttr("doctype-system"); v != "" {
		ss.DoctypeSystem = v
	}
	if v := el.MustAttr("media-type"); v != "" {
		ss.MediaType = v
	}
}

func compileVariable(el *dom.Node, isParam bool) (*Variable, error) {
	v := &Variable{
		Name:   el.MustAttr("name"),
		Select: el.MustAttr("select"),
		As:     el.MustAttr("as"),
		Body:   el,
	}
	if isParam {
		v.Required = strings.EqualFold(el.MustAttr("required"), "yes")
		v.Tunnel = strings.EqualFold(el.MustAttr("tunnel"), "yes")
	}
	if v.Select != "" {
		e, err := xpath.Parse(v.Select)
		if err != nil {
			return nil, fmt.Errorf("xslt: variable/param select %q: %w", v.Select, err)
		}
		v.SelectExpr = e
	}
	return v, nil
}

func compileKey(el *dom.Node) (*Key, error) {
	k := &Key{
		Name: el.MustAttr("name"),
		Use:  el.MustAttr("use"),
	}
	matchStr := el.MustAttr("match")
	if matchStr != "" {
		pat, err := xpath.CompilePattern(matchStr)
		if err != nil {
			return nil, fmt.Errorf("xslt: key match %q: %w", matchStr, err)
		}
		k.Match = pat
	}
	if k.Use != "" {
		e, err := xpath.Parse(k.Use)
		if err != nil {
			return nil, fmt.Errorf("xslt: key use %q: %w", k.Use, err)
		}
		k.UseExpr = e
	}
	return k, nil
}

func (ss *Stylesheet) compileFunction(el *dom.Node) (*Function, error) {
	fn := &Function{
		Name: el.MustAttr("name"),
		As:   el.MustAttr("as"),
		Body: el,
	}
	for _, child := range el.ChildElements() {
		if child.NamespaceURI == xslNS && child.LocalName == "param" {
			v, err := compileVariable(child, true)
			if err != nil {
				return nil, err
			}
			fn.Params = append(fn.Params, v)
		}
	}
	return fn, nil
}

func defaultDecimalFormat() *DecimalFormat {
	return &DecimalFormat{
		DecimalSeparator:  '.',
		GroupingSeparator: ',',
		Infinity:          "Infinity",
		MinusSign:         '-',
		NaN:               "NaN",
		Percent:           '%',
		PerMille:          '‰',
		ZeroDigit:         '0',
		Digit:             '#',
		PatternSeparator:  ';',
	}
}

// FindTemplate finds the best matching template for the given node and mode.
// Returns nil if no template matches (caller should use built-in template).
func (ss *Stylesheet) FindTemplate(n *dom.Node, mode string, ctx *xpath.Context) *Template {
	var best *Template
	bestPriority := -1000.0
	bestImport := -1

	for _, tmpl := range ss.Templates {
		if tmpl.Match == nil {
			continue
		}
		// Mode matching: '#all' matches any mode; '#default' matches no mode.
		tmplMode := tmpl.Mode
		if tmplMode == "#all" {
			// matches any mode
		} else if tmplMode != mode {
			continue
		}

		if !tmpl.Match.Matches(n, ctx) {
			continue
		}

		prio := tmpl.Priority
		imp := tmpl.ImportLevel

		if best == nil ||
			prio > bestPriority ||
			(prio == bestPriority && imp > bestImport) {
			best = tmpl
			bestPriority = prio
			bestImport = imp
		}
	}
	return best
}
