package xslt_test

import (
	"strings"
	"testing"

	"github.com/davewins/xslt"
)

// mustErr fails the test unless err is non-nil. The whole point of these tests
// is that hostile input is rejected with an error rather than crashing the
// process (Go stack overflow / OOM are unrecoverable).
func mustErr(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", what)
	}
}

// A self-recursive stylesheet previously produced an unrecoverable
// "fatal error: stack overflow". It must now return an error instead.
func TestRecursiveTemplateBounded(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r/>`)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><xsl:call-template name="loop"/></xsl:template>
  <xsl:template name="loop"><x><xsl:call-template name="loop"/></x></xsl:template>
</xsl:stylesheet>`)
	_, err := xslt.Transform(xml, style)
	mustErr(t, err, "recursive template")
}

// An attacker-controlled integer range must not allocate unbounded memory.
func TestRangeSizeBounded(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r/>`)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><out><xsl:value-of select="count(1 to 9999999999)"/></out></xsl:template>
</xsl:stylesheet>`)
	_, err := xslt.Transform(xml, style)
	mustErr(t, err, "range size")
}

// Deeply nested XML input must be rejected at parse time.
func TestDeepInputBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>`)
	depth := 20000
	for i := 0; i < depth; i++ {
		b.WriteString("<a>")
	}
	for i := 0; i < depth; i++ {
		b.WriteString("</a>")
	}
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><out><xsl:copy-of select="."/></out></xsl:template>
</xsl:stylesheet>`)
	_, err := xslt.Transform([]byte(b.String()), style)
	mustErr(t, err, "deep input")
}

// Untrusted data reaching xsl:comment must not be able to break out of the
// comment delimiters.
func TestCommentInjectionRejected(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r><c>--&gt;&lt;script&gt;alert(1)&lt;/script&gt;</c></r>`)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><out><xsl:comment><xsl:value-of select="/r/c"/></xsl:comment></out></xsl:template>
</xsl:stylesheet>`)
	_, err := xslt.Transform(xml, style)
	mustErr(t, err, "comment injection")
}

// Untrusted data reaching a processing-instruction must not be able to break out.
func TestPIInjectionRejected(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r><c>data?&gt;&lt;script/&gt;</c></r>`)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><out><xsl:processing-instruction name="php"><xsl:value-of select="/r/c"/></xsl:processing-instruction></out></xsl:template>
</xsl:stylesheet>`)
	_, err := xslt.Transform(xml, style)
	mustErr(t, err, "PI injection")
}

// A computed element name from untrusted data must not inject markup.
func TestElementNameInjectionRejected(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r><n>x&gt;&lt;script&gt;pwn&lt;/script&gt;</n></r>`)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><out><xsl:element name="{/r/n}">hi</xsl:element></out></xsl:template>
</xsl:stylesheet>`)
	_, err := xslt.Transform(xml, style)
	mustErr(t, err, "element-name injection")
}

// A pathologically nested XPath expression in the stylesheet must be rejected
// by the parser instead of overflowing the recursive-descent stack.
func TestDeepExpressionRejected(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r/>`)
	expr := strings.Repeat("(", 5000) + "1" + strings.Repeat(")", 5000)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><out><xsl:value-of select="` + expr + `"/></out></xsl:template>
</xsl:stylesheet>`)
	_, err := xslt.Transform(xml, style)
	mustErr(t, err, "deep expression")
}

// WithLimits must let callers raise a cap. A range of 5000 fails under a low
// limit but succeeds when the limit is raised.
func TestWithLimitsOverride(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r/>`)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/"><out><xsl:value-of select="count(1 to 5000)"/></out></xsl:template>
</xsl:stylesheet>`)

	if _, err := xslt.Transform(xml, style, xslt.WithLimits(xslt.Limits{MaxRangeSize: 100})); err == nil {
		t.Fatal("expected range-limit error under low MaxRangeSize")
	}

	out, err := xslt.Transform(xml, style, xslt.WithLimits(xslt.Limits{MaxRangeSize: 10000}))
	if err != nil {
		t.Fatalf("unexpected error under raised MaxRangeSize: %v", err)
	}
	if !strings.Contains(string(out), "5000") {
		t.Fatalf("expected count 5000 in output, got: %s", out)
	}
}

// Legitimate use of comments, PIs, and computed names (with valid values) must
// still work.
func TestValidConstructsStillWork(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><r><n>item</n></r>`)
	style := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" omit-xml-declaration="yes"/>
  <xsl:template match="/"><out><xsl:comment>ok comment</xsl:comment><xsl:element name="{/r/n}">hi</xsl:element></out></xsl:template>
</xsl:stylesheet>`)
	out, err := xslt.Transform(xml, style)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "<!--ok comment-->") || !strings.Contains(got, "<item>hi</item>") {
		t.Fatalf("unexpected output: %s", got)
	}
}
