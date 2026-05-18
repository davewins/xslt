package xslt

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/davewins/xslt/dom"
	"github.com/davewins/xslt/xpath"
)

// evalVariable evaluates a variable's value from select or body content.
func (tc *TransformContext) evalVariable(v *Variable, xctx *xpath.Context) (xpath.Sequence, error) {
	if v.SelectExpr != nil {
		return xpath.Eval(v.SelectExpr, xctx)
	}
	if v.Body != nil && hasNonParamChildren(v.Body) {
		// Evaluate body content into a result fragment.
		frag := &dom.Node{Type: dom.NodeDocument, Document: tc.ResultDoc}
		proc := &Processor{Stylesheet: tc.Stylesheet}
		if err := proc.executeBody(tc, xctx, v.Body, frag); err != nil {
			return nil, err
		}
		return xpath.Sequence{&xpath.NodeItem{Node: frag}}, nil
	}
	return xpath.Sequence{}, nil
}

func (tc *TransformContext) evalVariableBody(v *Variable, xctx *xpath.Context) (xpath.Sequence, error) {
	return tc.evalVariable(v, xctx)
}

func hasNonParamChildren(el *dom.Node) bool {
	for _, c := range el.Children {
		if c.Type == dom.NodeText && strings.TrimSpace(c.Value) != "" {
			return true
		}
		if c.Type == dom.NodeElement && !(c.NamespaceURI == xslNS && c.LocalName == "param") {
			return true
		}
	}
	return false
}

// formatNumber formats a number using an XSLT/Java-style picture string.
func formatNumber(num float64, picture string, df *DecimalFormat) string {
	if math.IsNaN(num) {
		return df.NaN
	}
	if math.IsInf(num, 1) {
		return df.Infinity
	}
	if math.IsInf(num, -1) {
		return "-" + df.Infinity
	}

	// Split picture on pattern separator (;) for positive/negative patterns.
	parts := splitOnRune(picture, df.PatternSeparator)
	posPat := parts[0]
	negPat := ""
	if len(parts) > 1 {
		negPat = parts[1]
	}

	negative := num < 0
	if negative {
		num = -num
	}

	pattern := posPat
	if negative && negPat != "" {
		pattern = negPat
	}

	// Check for percent/per-mille.
	if strings.ContainsRune(pattern, df.Percent) {
		num *= 100
	} else if strings.ContainsRune(pattern, df.PerMille) {
		num *= 1000
	}

	// Parse pattern to find decimal point, grouping, and digit counts.
	decIdx := strings.IndexRune(pattern, df.DecimalSeparator)
	var intPart, fracPart string
	if decIdx >= 0 {
		intPart = pattern[:decIdx]
		fracPart = pattern[decIdx+utf8.RuneLen(df.DecimalSeparator):]
	} else {
		intPart = pattern
	}

	// Count minimum integer digits (leading zeros).
	minIntDigits := 0
	for _, r := range intPart {
		if r == df.ZeroDigit {
			minIntDigits++
		}
	}

	// Count decimal digits.
	minFracDigits := 0
	maxFracDigits := 0
	for _, r := range fracPart {
		if r == df.ZeroDigit {
			minFracDigits++
			maxFracDigits++
		} else if r == df.Digit {
			maxFracDigits++
		}
	}

	// Format the number.
	var formatted string
	if maxFracDigits > 0 {
		formatted = strconv.FormatFloat(num, 'f', maxFracDigits, 64)
	} else {
		formatted = strconv.FormatFloat(math.Round(num), 'f', 0, 64)
	}

	// Split formatted number.
	var fmtInt, fmtFrac string
	if dotIdx := strings.Index(formatted, "."); dotIdx >= 0 {
		fmtInt = formatted[:dotIdx]
		fmtFrac = formatted[dotIdx+1:]
	} else {
		fmtInt = formatted
	}

	// Apply minimum integer digits padding.
	for len(fmtInt) < minIntDigits {
		fmtInt = string(df.ZeroDigit) + fmtInt
	}

	// Apply grouping separator.
	grpSep := df.GroupingSeparator
	grpSize := 0
	lastGrp := strings.LastIndexFunc(intPart, func(r rune) bool { return r == grpSep })
	if lastGrp >= 0 {
		// Count digits between last grouping separator and decimal point.
		afterGrp := intPart[lastGrp+utf8.RuneLen(grpSep):]
		for _, r := range afterGrp {
			if r == df.ZeroDigit || r == df.Digit {
				grpSize++
			}
		}
	}
	if grpSize > 0 && len(fmtInt) > grpSize {
		var sb strings.Builder
		n := len(fmtInt) % grpSize
		if n == 0 {
			n = grpSize
		}
		sb.WriteString(fmtInt[:n])
		for n < len(fmtInt) {
			sb.WriteRune(grpSep)
			sb.WriteString(fmtInt[n : n+grpSize])
			n += grpSize
		}
		fmtInt = sb.String()
	}

	// Truncate or pad fractional part.
	for len(fmtFrac) < minFracDigits {
		fmtFrac += string(df.ZeroDigit)
	}
	if len(fmtFrac) > maxFracDigits && maxFracDigits > 0 {
		fmtFrac = fmtFrac[:maxFracDigits]
	}

	// Reconstruct the number.
	var result strings.Builder
	// Add prefix (non-digit characters at start).
	for _, r := range pattern {
		if r == df.ZeroDigit || r == df.Digit || r == df.DecimalSeparator || r == df.GroupingSeparator {
			break
		}
		if r == df.Percent || r == df.PerMille {
			break
		}
		result.WriteRune(r)
	}
	if negative && negPat == "" {
		result.WriteRune(df.MinusSign)
	}
	result.WriteString(fmtInt)
	if maxFracDigits > 0 || fmtFrac != "" {
		result.WriteRune(df.DecimalSeparator)
		result.WriteString(fmtFrac)
	}
	// Add suffix (non-digit characters at end).
	suffixStart := false
	for _, r := range pattern {
		if !suffixStart {
			if r == df.ZeroDigit || r == df.Digit || r == df.DecimalSeparator {
				continue
			}
			if r == df.Percent {
				result.WriteRune(r)
				suffixStart = true
				continue
			}
			if r == df.PerMille {
				result.WriteRune(r)
				suffixStart = true
				continue
			}
		} else {
			result.WriteRune(r)
		}
	}

	return result.String()
}

func splitOnRune(s string, sep rune) []string {
	var parts []string
	start := 0
	for i, r := range s {
		if r == sep {
			parts = append(parts, s[start:i])
			start = i + utf8.RuneLen(sep)
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// compileRegex compiles an XPath/XSLT regex with flags.
func compileRegex(pattern, flags string) (*regexp.Regexp, error) {
	goFlags := "(?m)"
	if strings.Contains(flags, "s") {
		goFlags = "(?ms)"
	}
	if strings.Contains(flags, "i") {
		goFlags = goFlags[:len(goFlags)-1] + "i)"
	}
	return regexp.Compile(goFlags + pattern)
}

// newRegexp compiles a regular expression.
func newRegexp(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}

// generateID creates a unique ID for a DOM node.
func generateID(n *dom.Node) string {
	return fmt.Sprintf("N%d", n.DocOrder)
}
