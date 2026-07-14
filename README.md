# xslt2

A pure Go implementation of an XSLT 2.0 processor. Accepts XML and XSLT stylesheets as `[]byte` and returns the transformed output. No external dependencies — only the Go standard library.

## Installation

```bash
go get xslt2
```

## Quick start

```go
import "xslt2"

xml := []byte(`<?xml version="1.0"?>
<root>
  <item>Hello</item>
  <item>World</item>
</root>`)

xslt := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" omit-xml-declaration="yes"/>
  <xsl:template match="/">
    <output>
      <xsl:apply-templates select="root/item"/>
    </output>
  </xsl:template>
  <xsl:template match="item">
    <p><xsl:value-of select="."/></p>
  </xsl:template>
</xsl:stylesheet>`)

result, err := xslt2.Transform(xml, xslt)
// result: <output><p>Hello</p><p>World</p></output>
```

## API

### One-shot transform

```go
result, err := xslt2.Transform(xmlBytes, xsltBytes []byte) ([]byte, error)
```

Compiles the stylesheet and transforms the XML document in a single call. Use this when each call uses a different stylesheet.

### Reusable processor

```go
proc, err := xslt2.New(xsltBytes []byte) (*Processor, error)

result, err := proc.Transform(xmlBytes []byte) ([]byte, error)

result, err := proc.TransformWithParams(xmlBytes []byte, params map[string]interface{}) ([]byte, error)
```

`New` compiles the stylesheet once. Reuse the returned `*Processor` for repeated transformations against the same stylesheet — compilation is only paid once.

### Parameters

Top-level stylesheet parameters (`xsl:param`) can be supplied at transform time:

```go
proc, err := xslt2.New(xsltBytes)
result, err := proc.TransformWithParams(xmlBytes, map[string]interface{}{
    "lang":  "en",
    "count": 42,
})
```

Supported parameter types: `string`, `int`, `int64`, `float64`, `bool`, `[]byte` (parsed as an XML fragment).

### Messages and verbose output

```go
out, err := proc.TransformVerbose(xmlBytes, params)
// out.Output   []byte   — serialized result
// out.Messages []string — content of any xsl:message instructions
```

## Security and resource limits

Transformations are protected against denial-of-service from untrusted
stylesheets or documents. Safe defaults are applied automatically — **existing
code needs no changes** — and can be overridden with `WithLimits`:

```go
proc, err := xslt2.New(xsltBytes, xslt2.WithLimits(xslt2.Limits{
    MaxTemplateDepth: 20000,     // template/instruction recursion cap
    MaxNodeDepth:     10000,     // element nesting depth of parsed input
    MaxRangeSize:     50_000_000, // max items in an XPath range ("a to b")
}))
// Transform(xml, xslt, opts...) accepts the same options.
```

Any zero field keeps its default (`MaxTemplateDepth` 10000, `MaxNodeDepth` 5000,
`MaxRangeSize` 10,000,000). Exceeding a limit returns an error rather than
crashing the process (unbounded recursion would otherwise be an unrecoverable
Go stack overflow, and unbounded ranges an out-of-memory).

In addition, computed output is validated: an `xsl:comment`, `xsl:element` /
`xsl:attribute` name, or `xsl:processing-instruction` built from untrusted data
that would break out of its markup (e.g. `-->`, `?>`, or an illegal name) causes
the transform to return an error instead of emitting malformed/injected output.

## Supported XSLT 2.0 features

### Instructions

| Instruction | Notes |
|---|---|
| `xsl:template` | `match`, `name`, `mode`, `priority` |
| `xsl:apply-templates` | `select`, `mode`, with-param, sort |
| `xsl:call-template` | `name`, with-param |
| `xsl:for-each` | `select`, sort |
| `xsl:for-each-group` | `group-by`, `group-adjacent`, `group-starting-with`, `group-ending-with` |
| `xsl:if` | `test` |
| `xsl:choose` / `xsl:when` / `xsl:otherwise` | |
| `xsl:variable` | `name`, `select`, content body |
| `xsl:param` | `name`, `select`, `required`, `tunnel` |
| `xsl:with-param` | |
| `xsl:value-of` | `select`, `separator`, `disable-output-escaping` |
| `xsl:copy-of` | `select` |
| `xsl:copy` | shallow copy with body |
| `xsl:element` | `name`, `namespace`, `use-attribute-sets` |
| `xsl:attribute` | `name`, `namespace`, `select` |
| `xsl:namespace` | `name`, `select` |
| `xsl:text` | `disable-output-escaping` |
| `xsl:comment` | |
| `xsl:processing-instruction` | |
| `xsl:sequence` | `select` |
| `xsl:number` | `value`, `format` (decimal, roman `i`/`I`, alpha `a`/`A`) |
| `xsl:analyze-string` | `select`, `regex`, `flags`, matching/non-matching-substring |
| `xsl:message` | `select`, `terminate` |
| `xsl:sort` | `select`, `order`, `data-type`, `case-order` |
| `xsl:output` | `method`, `encoding`, `indent`, `omit-xml-declaration`, `doctype-public`, `doctype-system` |
| `xsl:key` | `name`, `match`, `use` |
| `xsl:attribute-set` | `name`, `use-attribute-sets` |
| `xsl:decimal-format` | |
| `xsl:function` | user-defined stylesheet functions |
| `xsl:strip-space` / `xsl:preserve-space` | |
| Literal result elements | with attribute value templates `{expr}` |

### XPath 2.0

The XPath engine supports the full XPath 2.0 expression language:

**Axes:** `child`, `descendant`, `descendant-or-self`, `attribute`, `self`, `parent`, `ancestor`, `ancestor-or-self`, `following-sibling`, `preceding-sibling`, `following`, `preceding`, `namespace`

**Node tests:** `node()`, `text()`, `comment()`, `processing-instruction()`, `element()`, `attribute()`, `document-node()`, name tests, wildcard `*`, `ns:*`

**Operators:** `=`, `!=`, `<`, `<=`, `>`, `>=`, `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `is`, `<<`, `>>`, `and`, `or`, `+`, `-`, `*`, `div`, `idiv`, `mod`, `to`, `union`/`|`, `intersect`, `except`, `instance of`, `cast as`, `castable as`

**Expressions:** `if`/`then`/`else`, `for`/`in`/`return`, `some`/`every`/`in`/`satisfies`, sequences `(a, b, c)`, predicates `[expr]`

**Built-in functions (selection):**

| Category | Functions |
|---|---|
| String | `string`, `concat`, `string-join`, `substring`, `string-length`, `normalize-space`, `upper-case`, `lower-case`, `contains`, `starts-with`, `ends-with`, `substring-before`, `substring-after`, `translate`, `replace`, `matches`, `tokenize`, `compare`, `encode-for-uri` |
| Numeric | `number`, `abs`, `floor`, `ceiling`, `round`, `round-half-to-even`, `format-number` |
| Sequence | `count`, `sum`, `avg`, `min`, `max`, `empty`, `exists`, `distinct-values`, `index-of`, `subsequence`, `reverse`, `insert-before`, `remove` |
| Node | `name`, `local-name`, `namespace-uri`, `root`, `id`, `generate-id`, `lang` |
| Boolean | `boolean`, `not`, `true`, `false` |
| Date/time | `current-dateTime`, `current-date`, `current-time` |
| XSLT-specific | `current`, `key`, `current-group`, `current-grouping-key`, `document`, `system-property`, `function-available` |

### Output methods

- `xml` — well-formed XML with namespace declarations
- `html` — HTML with void element handling (`<br>`, `<img>`, etc.)
- `text` — plain text (element markup stripped)

## Package structure

```
xslt2/
├── xslt2.go          Public API
├── dom/
│   ├── node.go       DOM node types and tree structure
│   └── parser.go     XML → DOM parser (namespace-aware)
├── xpath/
│   ├── token.go      Lexer token types
│   ├── lexer.go      XPath tokenizer
│   ├── ast.go        AST node types
│   ├── parser.go     Recursive-descent XPath 2.0 parser
│   ├── types.go      XPath 2.0 sequence and item types
│   ├── context.go    Evaluation context
│   ├── evaluator.go  Expression evaluator
│   ├── functions.go  Built-in XPath/XSLT functions
│   └── pattern.go    XSLT match pattern compiler
└── xslt/
    ├── stylesheet.go  Compiled stylesheet representation
    ├── context.go     XSLT transform context
    ├── processor.go   Main XSLT processor and instruction handlers
    ├── helpers.go     Variable evaluation, format-number, regex helpers
    └── serializer.go  Result tree serialization (XML/HTML/text)
```

## Limitations

- `xsl:import` and `xsl:include` do not load external files (no file I/O).
- `document()` and `doc()` functions return empty sequences for external URIs.
- XML Schema type validation (`xs:` types) is not enforced.
- No streaming or StAX-style processing — the full source document is held in memory.
