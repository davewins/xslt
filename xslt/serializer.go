package xslt

import (
	"bytes"
	"strings"

	"github.com/davewins/xslt/dom"
)

// Serialize serializes a result document to bytes according to the stylesheet's output settings.
func Serialize(doc *dom.Document, ss *Stylesheet) []byte {
	var buf bytes.Buffer

	switch strings.ToLower(ss.OutputMethod) {
	case "text":
		serializeText(doc.Root, &buf)
	case "html":
		if !ss.OmitXMLDecl {
			buf.WriteString("<!DOCTYPE html>\n")
		}
		serializeHTML(doc.Root, &buf, ss.OutputIndent, 0)
	default: // "xml"
		if !ss.OmitXMLDecl {
			enc := ss.OutputEncoding
			if enc == "" {
				enc = "UTF-8"
			}
			buf.WriteString(`<?xml version="1.0" encoding="`)
			buf.WriteString(enc)
			buf.WriteString(`"?>`)
			buf.WriteByte('\n')
		}
		if ss.DoctypePublic != "" || ss.DoctypeSystem != "" {
			serializeDoctype(&buf, doc.Root, ss)
		}
		serializeXML(doc.Root, &buf, ss.OutputIndent, 0, make(map[string]string))
	}
	return buf.Bytes()
}

func serializeDoctype(buf *bytes.Buffer, doc *dom.Node, ss *Stylesheet) {
	root := doc.FirstChildElement()
	if root == nil {
		return
	}
	buf.WriteString("<!DOCTYPE ")
	buf.WriteString(root.QName())
	if ss.DoctypePublic != "" {
		buf.WriteString(` PUBLIC "`)
		buf.WriteString(ss.DoctypePublic)
		buf.WriteByte('"')
	}
	if ss.DoctypeSystem != "" {
		buf.WriteString(` SYSTEM "`)
		buf.WriteString(ss.DoctypeSystem)
		buf.WriteByte('"')
	}
	buf.WriteString(">\n")
}

func serializeText(n *dom.Node, buf *bytes.Buffer) {
	switch n.Type {
	case dom.NodeDocument:
		for _, c := range n.Children {
			serializeText(c, buf)
		}
	case dom.NodeElement:
		for _, c := range n.Children {
			serializeText(c, buf)
		}
	case dom.NodeText:
		buf.WriteString(n.Value)
	}
}

// htmlVoidElements lists HTML5 void elements (self-closing).
var htmlVoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}

// htmlRawElements output their content without escaping.
var htmlRawElements = map[string]bool{
	"script": true, "style": true,
}

func serializeHTML(n *dom.Node, buf *bytes.Buffer, indent bool, depth int) {
	switch n.Type {
	case dom.NodeDocument:
		for _, c := range n.Children {
			serializeHTML(c, buf, indent, depth)
		}
	case dom.NodeElement:
		name := strings.ToLower(n.LocalName)
		if indent {
			writeIndent(buf, depth)
		}
		buf.WriteByte('<')
		buf.WriteString(n.QName())
		writeAttrs(n, buf, false)
		if htmlVoidElements[name] {
			buf.WriteByte('>')
			if indent {
				buf.WriteByte('\n')
			}
			return
		}
		buf.WriteByte('>')
		isRawElem := htmlRawElements[name]
		if indent && !isRawElem && len(n.Children) > 0 {
			buf.WriteByte('\n')
		}
		for _, c := range n.Children {
			if isRawElem && c.Type == dom.NodeText {
				buf.WriteString(c.Value)
			} else {
				serializeHTML(c, buf, indent && !isRawElem, depth+1)
			}
		}
		if indent && !isRawElem && len(n.Children) > 0 {
			writeIndent(buf, depth)
		}
		buf.WriteString("</")
		buf.WriteString(n.QName())
		buf.WriteByte('>')
		if indent {
			buf.WriteByte('\n')
		}
	case dom.NodeText:
		if isRaw(n) {
			buf.WriteString(n.Value)
		} else {
			writeEscapedHTML(buf, n.Value, false)
		}
	case dom.NodeComment:
		buf.WriteString("<!--")
		buf.WriteString(n.Value)
		buf.WriteString("-->")
		if indent {
			buf.WriteByte('\n')
		}
	case dom.NodeProcessingInstruction:
		buf.WriteString("<?")
		buf.WriteString(n.LocalName)
		if n.Value != "" {
			buf.WriteByte(' ')
			buf.WriteString(n.Value)
		}
		buf.WriteString("?>")
		if indent {
			buf.WriteByte('\n')
		}
	}
}

func serializeXML(n *dom.Node, buf *bytes.Buffer, indent bool, depth int, parentNS map[string]string) {
	switch n.Type {
	case dom.NodeDocument:
		for _, c := range n.Children {
			serializeXML(c, buf, indent, depth, parentNS)
		}
	case dom.NodeElement:
		if indent {
			writeIndent(buf, depth)
		}
		buf.WriteByte('<')
		buf.WriteString(n.QName())

		// Write namespace declarations.
		// Track which namespaces are new relative to parent scope.
		newNS := make(map[string]string)
		for k, v := range parentNS {
			newNS[k] = v
		}
		for _, ns := range n.Namespaces {
			if ns.Prefix == "xml" {
				continue // xml prefix is implicit per XML spec
			}
			if parentNS[ns.Prefix] != ns.URI {
				// Write namespace declaration.
				buf.WriteString(" xmlns")
				if ns.Prefix != "" {
					buf.WriteByte(':')
					buf.WriteString(ns.Prefix)
				}
				buf.WriteString(`="`)
				writeEscapedXML(buf, ns.URI, true)
				buf.WriteByte('"')
				newNS[ns.Prefix] = ns.URI
			}
		}
		// Ensure the element's own namespace is declared.
		if n.NamespaceURI != "" {
			declared := false
			for _, ns := range n.Namespaces {
				if ns.URI == n.NamespaceURI && ns.Prefix == n.Prefix {
					declared = true
					break
				}
			}
			if !declared && parentNS[n.Prefix] != n.NamespaceURI {
				buf.WriteString(" xmlns")
				if n.Prefix != "" {
					buf.WriteByte(':')
					buf.WriteString(n.Prefix)
				}
				buf.WriteString(`="`)
				writeEscapedXML(buf, n.NamespaceURI, true)
				buf.WriteByte('"')
				newNS[n.Prefix] = n.NamespaceURI
			}
		}

		writeAttrs(n, buf, true)

		if len(n.Children) == 0 {
			buf.WriteString("/>")
			if indent {
				buf.WriteByte('\n')
			}
			return
		}
		buf.WriteByte('>')

		// Determine if we have only text children (don't indent those).
		hasElemChildren := false
		for _, c := range n.Children {
			if c.Type == dom.NodeElement || c.Type == dom.NodeComment || c.Type == dom.NodeProcessingInstruction {
				hasElemChildren = true
				break
			}
		}
		if indent && hasElemChildren {
			buf.WriteByte('\n')
		}
		for _, c := range n.Children {
			serializeXML(c, buf, indent && hasElemChildren, depth+1, newNS)
		}
		if indent && hasElemChildren {
			writeIndent(buf, depth)
		}
		buf.WriteString("</")
		buf.WriteString(n.QName())
		buf.WriteByte('>')
		if indent {
			buf.WriteByte('\n')
		}

	case dom.NodeText:
		if isRaw(n) {
			buf.WriteString(n.Value)
		} else {
			writeEscapedXML(buf, n.Value, false)
		}
	case dom.NodeComment:
		if indent {
			writeIndent(buf, depth)
		}
		buf.WriteString("<!--")
		buf.WriteString(n.Value)
		buf.WriteString("-->")
		if indent {
			buf.WriteByte('\n')
		}
	case dom.NodeProcessingInstruction:
		if indent {
			writeIndent(buf, depth)
		}
		buf.WriteString("<?")
		buf.WriteString(n.LocalName)
		if n.Value != "" {
			buf.WriteByte(' ')
			buf.WriteString(n.Value)
		}
		buf.WriteString("?>")
		if indent {
			buf.WriteByte('\n')
		}
	}
}

func writeAttrs(n *dom.Node, buf *bytes.Buffer, xml bool) {
	for _, a := range n.Attributes {
		if a.LocalName == "raw" && a.NamespaceURI == "" {
			continue // internal marker
		}
		buf.WriteByte(' ')
		buf.WriteString(a.QName())
		buf.WriteString(`="`)
		if xml {
			writeEscapedXML(buf, a.Value, true)
		} else {
			writeEscapedHTML(buf, a.Value, true)
		}
		buf.WriteByte('"')
	}
}

func writeEscapedXML(buf *bytes.Buffer, s string, isAttr bool) {
	for _, r := range s {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			if isAttr {
				buf.WriteByte('>')
			} else {
				buf.WriteString("&gt;")
			}
		case '"':
			if isAttr {
				buf.WriteString("&quot;")
			} else {
				buf.WriteRune(r)
			}
		default:
			buf.WriteRune(r)
		}
	}
}

func writeEscapedHTML(buf *bytes.Buffer, s string, isAttr bool) {
	writeEscapedXML(buf, s, isAttr)
}

func writeIndent(buf *bytes.Buffer, depth int) {
	for i := 0; i < depth; i++ {
		buf.WriteString("  ")
	}
}
