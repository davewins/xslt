// Package xslt2 provides an XSLT 2.0 processor implemented in pure Go.
//
// Basic usage:
//
//	result, err := xslt2.Transform(xmlBytes, xsltBytes)
//
// Or with a reusable processor:
//
//	proc, err := xslt2.New(xsltBytes)
//	result, err := proc.Transform(xmlBytes)
//	result, err = proc.TransformWithParams(xmlBytes, map[string]interface{}{"key": "value"})
package xslt

import (
	"fmt"

	"github.com/davewins/xslt/dom"
	"github.com/davewins/xslt/xpath"
	"github.com/davewins/xslt/xslt"
)

// Limits bounds the resources a transformation may consume (recursion depth,
// input nesting depth, XPath range size), guarding against denial-of-service
// from untrusted stylesheets or documents. A zero field means "use the default".
// See xslt.DefaultLimits for the default values.
type Limits = xslt.Limits

// Option configures a Processor created with New.
type Option func(*options)

type options struct {
	limits Limits
}

// WithLimits overrides the default resource limits. Any zero field in l keeps
// its default value.
func WithLimits(l Limits) Option {
	return func(o *options) { o.limits = l }
}

// Processor is a compiled XSLT 2.0 stylesheet ready to transform XML documents.
type Processor struct {
	proc   *xslt.Processor
	ss     *xslt.Stylesheet
	limits Limits
}

// New compiles an XSLT 2.0 stylesheet and returns a Processor.
// The stylesheet bytes must be a well-formed XML document with the
// xsl:stylesheet or xsl:transform element in the XSLT 2.0 namespace.
//
// Without options, safe default resource limits are applied (see Limits and
// xslt.DefaultLimits). Use WithLimits to override them.
func New(xsltBytes []byte, opts ...Option) (*Processor, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	limits := o.limits.WithDefaults()

	xsltDoc, err := dom.ParseLimited(xsltBytes, limits.MaxNodeDepth)
	if err != nil {
		return nil, fmt.Errorf("xslt2: parse stylesheet: %w", err)
	}
	ss, err := xslt.Compile(xsltDoc)
	if err != nil {
		return nil, err
	}
	return &Processor{
		proc:   xslt.NewProcessor(ss, limits),
		ss:     ss,
		limits: limits,
	}, nil
}

// Transform applies the stylesheet to an XML document and returns the serialized result.
func (p *Processor) Transform(xmlBytes []byte) ([]byte, error) {
	return p.TransformWithParams(xmlBytes, nil)
}

// TransformWithParams applies the stylesheet with top-level parameter values.
// Parameter values can be strings, int64, float64, bool, or []byte (XML fragment).
func (p *Processor) TransformWithParams(xmlBytes []byte, params map[string]interface{}) ([]byte, error) {
	srcDoc, err := dom.ParseLimited(xmlBytes, p.limits.MaxNodeDepth)
	if err != nil {
		return nil, fmt.Errorf("xslt2: parse source document: %w", err)
	}

	xparams := make(map[string]xpath.Sequence, len(params))
	for k, v := range params {
		xparams[k] = toSequence(v, p.limits.MaxNodeDepth)
	}

	resultDoc, messages, err := p.proc.Transform(srcDoc, xparams)
	if err != nil {
		return nil, err
	}
	_ = messages // caller can access messages via TransformVerbose

	return xslt.Serialize(resultDoc, p.ss), nil
}

// TransformResult holds the output of a transformation.
type TransformResult struct {
	// Output is the serialized transformation result.
	Output []byte
	// Messages contains any xsl:message output.
	Messages []string
}

// TransformVerbose performs a transformation and returns all output including messages.
func (p *Processor) TransformVerbose(xmlBytes []byte, params map[string]interface{}) (*TransformResult, error) {
	srcDoc, err := dom.ParseLimited(xmlBytes, p.limits.MaxNodeDepth)
	if err != nil {
		return nil, fmt.Errorf("xslt2: parse source document: %w", err)
	}
	xparams := make(map[string]xpath.Sequence, len(params))
	for k, v := range params {
		xparams[k] = toSequence(v, p.limits.MaxNodeDepth)
	}
	resultDoc, messages, err := p.proc.Transform(srcDoc, xparams)
	if err != nil {
		return &TransformResult{Messages: messages}, err
	}
	return &TransformResult{
		Output:   xslt.Serialize(resultDoc, p.ss),
		Messages: messages,
	}, nil
}

// Transform is a convenience function that compiles the stylesheet and transforms the XML document.
// Use New() for repeated transformations with the same stylesheet.
func Transform(xmlBytes, xsltBytes []byte, opts ...Option) ([]byte, error) {
	p, err := New(xsltBytes, opts...)
	if err != nil {
		return nil, err
	}
	return p.Transform(xmlBytes)
}

// toSequence converts a Go value to an XPath 2.0 Sequence. []byte values are
// parsed as XML fragments, bounded by maxDepth.
func toSequence(v interface{}, maxDepth int) xpath.Sequence {
	if v == nil {
		return xpath.Sequence{}
	}
	switch val := v.(type) {
	case string:
		return xpath.Sequence{xpath.StringItem(val)}
	case int:
		return xpath.Sequence{xpath.IntegerItem(int64(val))}
	case int64:
		return xpath.Sequence{xpath.IntegerItem(val)}
	case float64:
		return xpath.Sequence{xpath.DoubleItem(val)}
	case bool:
		return xpath.Sequence{xpath.BoolItem(val)}
	case []byte:
		// Treat as XML fragment — parse and return as node.
		doc, err := dom.ParseLimited(val, maxDepth)
		if err != nil {
			return xpath.Sequence{xpath.StringItem(string(val))}
		}
		return xpath.Sequence{&xpath.NodeItem{Node: doc.Root}}
	case xpath.Sequence:
		return val
	default:
		return xpath.Sequence{xpath.StringItem(fmt.Sprintf("%v", val))}
	}
}
