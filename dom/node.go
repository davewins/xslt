package dom

import "strings"

// NodeType enumerates the XML/XPath node kinds.
type NodeType int

const (
	NodeDocument NodeType = iota + 1
	NodeElement
	NodeText
	NodeAttribute
	NodeComment
	NodeProcessingInstruction
	NodeNamespace
)

// Namespace holds a single namespace binding (prefix → URI).
type Namespace struct {
	Prefix string
	URI    string
}

// Node is a single node in the DOM tree.
type Node struct {
	Type         NodeType
	LocalName    string
	Prefix       string
	NamespaceURI string
	Value        string     // text content for text/comment/PI/attribute/namespace nodes
	Attributes   []*Node    // attribute nodes (on element nodes)
	Namespaces   []Namespace // in-scope namespace bindings (on element nodes)
	Children     []*Node
	Parent       *Node
	Document     *Document
	DocOrder     int // document-order position (1-based)
}

// Document is the owning document of a node tree.
type Document struct {
	Root    *Node
	nodeSeq int
}

// NewDocument creates an empty document with a document-node root.
func NewDocument() *Document {
	d := &Document{nodeSeq: 1}
	d.Root = &Node{Type: NodeDocument, Document: d, DocOrder: 1}
	return d
}

// NextOrder allocates the next document-order number.
func (d *Document) NextOrder() int {
	d.nodeSeq++
	return d.nodeSeq
}

// QName returns the qualified name (prefix:local or just local).
func (n *Node) QName() string {
	if n.Prefix != "" {
		return n.Prefix + ":" + n.LocalName
	}
	return n.LocalName
}

// StringValue returns the XPath string value of the node.
func (n *Node) StringValue() string {
	switch n.Type {
	case NodeDocument, NodeElement:
		var b strings.Builder
		gatherText(n, &b)
		return b.String()
	default:
		return n.Value
	}
}

func gatherText(n *Node, b *strings.Builder) {
	for _, c := range n.Children {
		if c.Type == NodeText {
			b.WriteString(c.Value)
		} else if c.Type == NodeElement {
			gatherText(c, b)
		}
	}
}

// Attr returns (value, true) for the first attribute matching localName with no namespace.
func (n *Node) Attr(localName string) (string, bool) {
	for _, a := range n.Attributes {
		if a.LocalName == localName && a.NamespaceURI == "" {
			return a.Value, true
		}
	}
	return "", false
}

// AttrNS returns (value, true) for the first attribute matching ns+localName.
func (n *Node) AttrNS(ns, localName string) (string, bool) {
	for _, a := range n.Attributes {
		if a.NamespaceURI == ns && a.LocalName == localName {
			return a.Value, true
		}
	}
	return "", false
}

// MustAttr returns the attribute value or empty string.
func (n *Node) MustAttr(localName string) string {
	v, _ := n.Attr(localName)
	return v
}

// MustAttrNS returns the namespaced attribute value or empty string.
func (n *Node) MustAttrNS(ns, localName string) string {
	v, _ := n.AttrNS(ns, localName)
	return v
}

// ChildElements returns all direct child element nodes.
func (n *Node) ChildElements() []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Type == NodeElement {
			out = append(out, c)
		}
	}
	return out
}

// FirstChildElement returns the first child element or nil.
func (n *Node) FirstChildElement() *Node {
	for _, c := range n.Children {
		if c.Type == NodeElement {
			return c
		}
	}
	return nil
}

// LookupPrefix returns the namespace URI bound to prefix in scope at this node.
func (n *Node) LookupPrefix(prefix string) string {
	for _, ns := range n.Namespaces {
		if ns.Prefix == prefix {
			return ns.URI
		}
	}
	return ""
}

// DocumentElement returns the root element child of the document node.
func (d *Document) DocumentElement() *Node {
	if d.Root == nil {
		return nil
	}
	return d.Root.FirstChildElement()
}
