package xslt_test

import (
	"strings"
	"testing"

	"github.com/davewins/xslt"
)

func TestBasicTransform(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?>
<root>
  <item>Hello</item>
  <item>World</item>
</root>`)

	xsltDoc := []byte(`<?xml version="1.0"?>
<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" indent="no" omit-xml-declaration="yes"/>
  <xsl:template match="/">
    <output>
      <xsl:apply-templates select="root/item"/>
    </output>
  </xsl:template>
  <xsl:template match="item">
    <p><xsl:value-of select="."/></p>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	want := `<output><p>Hello</p><p>World</p></output>`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestForEach(t *testing.T) {
	xml := []byte(`<items><item n="1"/><item n="2"/><item n="3"/></items>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:template match="/">
    <xsl:for-each select="items/item">
      <xsl:value-of select="@n"/>
    </xsl:for-each>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	if got != "123" {
		t.Errorf("for-each: got %q, want %q", got, "123")
	}
}

func TestChoose(t *testing.T) {
	xml := []byte(`<root><val>5</val></root>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:template match="/">
    <xsl:variable name="v" select="number(root/val)"/>
    <xsl:choose>
      <xsl:when test="$v &gt; 3">big</xsl:when>
      <xsl:otherwise>small</xsl:otherwise>
    </xsl:choose>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	if got != "big" {
		t.Errorf("choose: got %q, want %q", got, "big")
	}
}

func TestSort(t *testing.T) {
	xml := []byte(`<items><item>banana</item><item>apple</item><item>cherry</item></items>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:template match="/">
    <xsl:for-each select="items/item">
      <xsl:sort select="."/>
      <xsl:value-of select="."/>
      <xsl:if test="position() != last()">,</xsl:if>
    </xsl:for-each>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	want := "apple,banana,cherry"
	if got != want {
		t.Errorf("sort: got %q, want %q", got, want)
	}
}

func TestVariables(t *testing.T) {
	xml := []byte(`<root/>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:template match="/">
    <xsl:variable name="greeting" select="'Hello, World!'"/>
    <xsl:value-of select="$greeting"/>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	if got != "Hello, World!" {
		t.Errorf("variable: got %q, want %q", got, "Hello, World!")
	}
}

func TestXPathFunctions(t *testing.T) {
	xml := []byte(`<root><a>hello</a><b>world</b></root>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:template match="/">
    <xsl:value-of select="upper-case(root/a)"/>
    <xsl:text> </xsl:text>
    <xsl:value-of select="string-length(root/b)"/>
    <xsl:text> </xsl:text>
    <xsl:value-of select="count(root/*)"/>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	want := "HELLO 5 2"
	if got != want {
		t.Errorf("xpath functions: got %q, want %q", got, want)
	}
}

func TestCopyOf(t *testing.T) {
	xml := []byte(`<root><inner attr="val">text</inner></root>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" omit-xml-declaration="yes"/>
  <xsl:template match="/">
    <result>
      <xsl:copy-of select="root/inner"/>
    </result>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	if !strings.Contains(got, `<inner attr="val">text</inner>`) {
		t.Errorf("copy-of: unexpected output: %s", got)
	}
}

func TestForEachGroup(t *testing.T) {
	xml := []byte(`<items>
  <item cat="a">1</item>
  <item cat="b">2</item>
  <item cat="a">3</item>
  <item cat="b">4</item>
</items>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:template match="/">
    <xsl:for-each-group select="items/item" group-by="@cat">
      <xsl:sort select="current-grouping-key()"/>
      <xsl:value-of select="current-grouping-key()"/>
      <xsl:text>:</xsl:text>
      <xsl:value-of select="count(current-group())"/>
      <xsl:text> </xsl:text>
    </xsl:for-each-group>
  </xsl:template>
</xsl:stylesheet>`)

	result, err := xslt.Transform(xml, xsltDoc)
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	if !strings.Contains(got, "a:2") || !strings.Contains(got, "b:2") {
		t.Errorf("for-each-group: unexpected output: %q", got)
	}
}

func TestParams(t *testing.T) {
	xml := []byte(`<root/>`)
	xsltDoc := []byte(`<xsl:stylesheet version="2.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:param name="name" select="'default'"/>
  <xsl:template match="/">
    <xsl:value-of select="$name"/>
  </xsl:template>
</xsl:stylesheet>`)

	proc, err := xslt.New(xsltDoc)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	result, err := proc.TransformWithParams(xml, map[string]interface{}{"name": "custom"})
	if err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	got := strings.TrimSpace(string(result))
	if got != "custom" {
		t.Errorf("params: got %q, want %q", got, "custom")
	}
}
