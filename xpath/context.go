package xpath

import "xslt2/dom"

// Function is the signature for a built-in or user-defined XPath function.
type Function func(ctx *Context, args []Sequence) (Sequence, error)

// Context holds the XPath dynamic evaluation context.
type Context struct {
	Item     Item                 // context item (.)
	Position int                  // 1-based position
	Size     int                  // size of the context sequence
	Vars     map[string]Sequence  // variable bindings (local name → value)
	Funcs    map[string]Function  // function library (name → handler)
	NSMap    map[string]string    // namespace prefix → URI
	Doc      *dom.Document        // the source document
}

// NewContext creates an empty evaluation context.
func NewContext(doc *dom.Document) *Context {
	return &Context{
		Vars:  make(map[string]Sequence),
		Funcs: make(map[string]Function),
		NSMap: make(map[string]string),
		Doc:   doc,
	}
}

// Sub returns a derived context with the given item/position/size.
func (c *Context) Sub(item Item, pos, size int) *Context {
	return &Context{
		Item:     item,
		Position: pos,
		Size:     size,
		Vars:     c.Vars,
		Funcs:    c.Funcs,
		NSMap:    c.NSMap,
		Doc:      c.Doc,
	}
}

// WithVar returns a derived context with an additional variable binding.
func (c *Context) WithVar(name string, val Sequence) *Context {
	newVars := make(map[string]Sequence, len(c.Vars)+1)
	for k, v := range c.Vars {
		newVars[k] = v
	}
	newVars[name] = val
	return &Context{
		Item:     c.Item,
		Position: c.Position,
		Size:     c.Size,
		Vars:     newVars,
		Funcs:    c.Funcs,
		NSMap:    c.NSMap,
		Doc:      c.Doc,
	}
}

// ResolveNamespace resolves a prefix to a URI using the context namespace map.
func (c *Context) ResolveNamespace(prefix string) string {
	if prefix == "xml" {
		return "http://www.w3.org/XML/1998/namespace"
	}
	if prefix == "xs" {
		return "http://www.w3.org/2001/XMLSchema"
	}
	return c.NSMap[prefix]
}

// LookupFunc looks up a function by qualified name.
func (c *Context) LookupFunc(prefix, local string) (Function, bool) {
	// Try with empty prefix first (built-ins registered without prefix).
	if f, ok := c.Funcs[local]; ok {
		return f, true
	}
	if prefix != "" {
		ns := c.ResolveNamespace(prefix)
		key := ns + ":" + local
		if f, ok := c.Funcs[key]; ok {
			return f, true
		}
		key = prefix + ":" + local
		if f, ok := c.Funcs[key]; ok {
			return f, true
		}
	}
	return nil, false
}

// CurrentNode returns the context node, or nil if the context item is not a node.
func (c *Context) CurrentNode() *dom.Node {
	if c.Item != nil && c.Item.IsNode() {
		return c.Item.(*NodeItem).Node
	}
	return nil
}
