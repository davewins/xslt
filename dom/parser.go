package dom

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

// Parse parses raw XML bytes into a Document.
// It uses encoding/xml's RawToken so that namespace prefixes are preserved.
func Parse(data []byte) (*Document, error) {
	doc := NewDocument()
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	stack := []*Node{doc.Root}
	// nsStack tracks namespace bindings per nesting level: prefix → URI
	nsStack := []map[string]string{
		{"xml": "http://www.w3.org/XML/1998/namespace"},
	}

	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("dom: XML parse error: %w", err)
		}

		parent := stack[len(stack)-1]
		parentNS := nsStack[len(nsStack)-1]

		switch t := tok.(type) {
		case xml.StartElement:
			// Build a new namespace scope, inheriting from parent.
			newScope := make(map[string]string, len(parentNS)+4)
			for k, v := range parentNS {
				newScope[k] = v
			}
			// Collect namespace declarations from this element's attributes.
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					newScope[a.Name.Local] = a.Value
				} else if a.Name.Space == "" && a.Name.Local == "xmlns" {
					newScope[""] = a.Value
				}
			}

			// Resolve element namespace.
			prefix := t.Name.Space // RawToken: Space = prefix
			nsURI := ""
			if prefix != "" {
				nsURI = newScope[prefix]
			} else {
				nsURI = newScope[""] // default namespace
			}

			elem := &Node{
				Type:         NodeElement,
				LocalName:    t.Name.Local,
				Prefix:       prefix,
				NamespaceURI: nsURI,
				Parent:       parent,
				Document:     doc,
				DocOrder:     doc.NextOrder(),
			}

			// Populate in-scope namespaces as a slice.
			for pfx, uri := range newScope {
				elem.Namespaces = append(elem.Namespaces, Namespace{Prefix: pfx, URI: uri})
			}

			// Process attributes.
			for _, a := range t.Attr {
				// Skip xmlns declarations (handled above).
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				attrPrefix := a.Name.Space
				attrNS := ""
				if attrPrefix != "" {
					attrNS = newScope[attrPrefix]
				}
				// Attributes without a prefix do NOT inherit the default namespace.
				attr := &Node{
					Type:         NodeAttribute,
					LocalName:    a.Name.Local,
					Prefix:       attrPrefix,
					NamespaceURI: attrNS,
					Value:        a.Value,
					Parent:       elem,
					Document:     doc,
					DocOrder:     doc.NextOrder(),
				}
				elem.Attributes = append(elem.Attributes, attr)
			}

			parent.Children = append(parent.Children, elem)
			stack = append(stack, elem)
			nsStack = append(nsStack, newScope)

		case xml.EndElement:
			stack = stack[:len(stack)-1]
			nsStack = nsStack[:len(nsStack)-1]

		case xml.CharData:
			text := &Node{
				Type:     NodeText,
				Value:    string(t),
				Parent:   parent,
				Document: doc,
				DocOrder: doc.NextOrder(),
			}
			parent.Children = append(parent.Children, text)

		case xml.Comment:
			comment := &Node{
				Type:     NodeComment,
				Value:    string(t),
				Parent:   parent,
				Document: doc,
				DocOrder: doc.NextOrder(),
			}
			parent.Children = append(parent.Children, comment)

		case xml.ProcInst:
			pi := &Node{
				Type:      NodeProcessingInstruction,
				LocalName: t.Target,
				Value:     string(t.Inst),
				Parent:    parent,
				Document:  doc,
				DocOrder:  doc.NextOrder(),
			}
			parent.Children = append(parent.Children, pi)
		}
	}

	return doc, nil
}

// Serialize writes a node tree to a byte slice.
// This is a simple round-trip serializer used internally.
func Serialize(n *Node) []byte {
	var b bytes.Buffer
	serializeNode(n, &b, false)
	return b.Bytes()
}

func serializeNode(n *Node, b *bytes.Buffer, omitDecl bool) {
	switch n.Type {
	case NodeDocument:
		if !omitDecl {
			b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
		}
		for _, c := range n.Children {
			serializeNode(c, b, false)
		}
	case NodeElement:
		b.WriteByte('<')
		b.WriteString(n.QName())
		for _, a := range n.Attributes {
			b.WriteByte(' ')
			b.WriteString(a.QName())
			b.WriteString(`="`)
			writeEscaped(b, a.Value, true)
			b.WriteByte('"')
		}
		if len(n.Children) == 0 {
			b.WriteString("/>")
		} else {
			b.WriteByte('>')
			for _, c := range n.Children {
				serializeNode(c, b, false)
			}
			b.WriteString("</")
			b.WriteString(n.QName())
			b.WriteByte('>')
		}
	case NodeText:
		writeEscaped(b, n.Value, false)
	case NodeComment:
		b.WriteString("<!--")
		b.WriteString(n.Value)
		b.WriteString("-->")
	case NodeProcessingInstruction:
		b.WriteString("<?")
		b.WriteString(n.LocalName)
		if n.Value != "" {
			b.WriteByte(' ')
			b.WriteString(n.Value)
		}
		b.WriteString("?>")
	}
}

func writeEscaped(b *bytes.Buffer, s string, isAttr bool) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			if !isAttr {
				b.WriteString("&gt;")
			} else {
				b.WriteByte('>')
			}
		case '"':
			if isAttr {
				b.WriteString("&quot;")
			} else {
				b.WriteByte('"')
			}
		default:
			b.WriteRune(r)
		}
	}
}
