package xpath

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"xslt2/dom"
)

// RegisterBuiltins populates a function map with all standard XPath 2.0 functions.
func RegisterBuiltins(funcs map[string]Function) {
	// Type/conversion
	funcs["string"] = fnString
	funcs["number"] = fnNumber
	funcs["boolean"] = fnBoolean
	funcs["integer"] = fnInteger
	funcs["float"] = fnFloat
	funcs["double"] = fnDouble

	// String functions
	funcs["concat"] = fnConcat
	funcs["string-join"] = fnStringJoin
	funcs["contains"] = fnContains
	funcs["starts-with"] = fnStartsWith
	funcs["ends-with"] = fnEndsWith
	funcs["substring"] = fnSubstring
	funcs["substring-before"] = fnSubstringBefore
	funcs["substring-after"] = fnSubstringAfter
	funcs["string-length"] = fnStringLength
	funcs["normalize-space"] = fnNormalizeSpace
	funcs["upper-case"] = fnUpperCase
	funcs["lower-case"] = fnLowerCase
	funcs["translate"] = fnTranslate
	funcs["replace"] = fnReplace
	funcs["matches"] = fnMatches
	funcs["tokenize"] = fnTokenize
	funcs["compare"] = fnCompare
	funcs["codepoints-to-string"] = fnCodepointsToString
	funcs["string-to-codepoints"] = fnStringToCodepoints
	funcs["encode-for-uri"] = fnEncodeForURI
	funcs["normalize-unicode"] = fnNormalizeUnicode

	// Boolean
	funcs["not"] = fnNot
	funcs["true"] = fnTrue
	funcs["false"] = fnFalse

	// Numeric
	funcs["abs"] = fnAbs
	funcs["floor"] = fnFloor
	funcs["ceiling"] = fnCeiling
	funcs["round"] = fnRound
	funcs["round-half-to-even"] = fnRoundHalfToEven

	// Aggregate / sequence
	funcs["count"] = fnCount
	funcs["sum"] = fnSum
	funcs["avg"] = fnAvg
	funcs["min"] = fnMin
	funcs["max"] = fnMax
	funcs["position"] = fnPosition
	funcs["last"] = fnLast
	funcs["empty"] = fnEmpty
	funcs["exists"] = fnExists
	funcs["distinct-values"] = fnDistinctValues
	funcs["index-of"] = fnIndexOf
	funcs["insert-before"] = fnInsertBefore
	funcs["remove"] = fnRemove
	funcs["reverse"] = fnReverse
	funcs["subsequence"] = fnSubsequence
	funcs["unordered"] = fnUnordered
	funcs["zero-or-one"] = fnZeroOrOne
	funcs["one-or-more"] = fnOneOrMore
	funcs["exactly-one"] = fnExactlyOne
	funcs["data"] = fnData
	funcs["deep-equal"] = fnDeepEqual

	// Node functions
	funcs["name"] = fnName
	funcs["local-name"] = fnLocalName
	funcs["namespace-uri"] = fnNamespaceURI
	funcs["node-name"] = fnNodeName
	funcs["nilled"] = fnNilled
	funcs["base-uri"] = fnBaseURI
	funcs["document-uri"] = fnDocumentURI
	funcs["root"] = fnRoot
	funcs["lang"] = fnLang
	funcs["id"] = fnId
	funcs["idref"] = fnIdRef
	funcs["generate-id"] = fnGenerateId

	// Date/time
	funcs["current-dateTime"] = fnCurrentDateTime
	funcs["current-date"] = fnCurrentDate
	funcs["current-time"] = fnCurrentTime
	funcs["implicit-timezone"] = fnImplicitTimezone

	// Misc
	funcs["error"] = fnError
	funcs["trace"] = fnTrace
	funcs["static-base-uri"] = fnStaticBaseURI
	funcs["function-available"] = fnFunctionAvailable
	funcs["element-available"] = fnElementAvailable
	funcs["type-available"] = fnTypeAvailable
	funcs["system-property"] = fnSystemProperty
}

// ---- Helpers ----

func strArg(args []Sequence, i int, ctx *Context) string {
	if i >= len(args) {
		if ctx.Item != nil {
			return ctx.Item.StringValue()
		}
		return ""
	}
	return StringValue(args[i])
}

func numArg(args []Sequence, i int) float64 {
	if i >= len(args) {
		return math.NaN()
	}
	return NumberValue(args[i])
}

// ---- Type functions ----

func fnString(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	return Sequence{StringItem(s)}, nil
}

func fnNumber(ctx *Context, args []Sequence) (Sequence, error) {
	var f float64
	if len(args) == 0 {
		if ctx.Item != nil {
			f, _ = TypedNumber(ctx.Item)
		} else {
			f = math.NaN()
		}
	} else {
		f = NumberValue(args[0])
	}
	return Sequence{DoubleItem(f)}, nil
}

func fnBoolean(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{BoolItem(false)}, nil
	}
	return Sequence{BoolItem(BooleanValue(args[0]))}, nil
}

func fnInteger(ctx *Context, args []Sequence) (Sequence, error) {
	f := numArg(args, 0)
	return Sequence{IntegerItem(int64(f))}, nil
}

func fnFloat(ctx *Context, args []Sequence) (Sequence, error) {
	f := numArg(args, 0)
	return Sequence{DoubleItem(f)}, nil
}

func fnDouble(ctx *Context, args []Sequence) (Sequence, error) {
	f := numArg(args, 0)
	return Sequence{DoubleItem(f)}, nil
}

// ---- String functions ----

func fnConcat(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("concat() requires at least 2 arguments")
	}
	var sb strings.Builder
	for _, a := range args {
		sb.WriteString(StringValue(a))
	}
	return Sequence{StringItem(sb.String())}, nil
}

func fnStringJoin(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{StringItem("")}, nil
	}
	sep := ""
	if len(args) >= 2 {
		sep = StringValue(args[1])
	}
	var parts []string
	for _, item := range args[0] {
		parts = append(parts, item.StringValue())
	}
	return Sequence{StringItem(strings.Join(parts, sep))}, nil
}

func fnContains(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	sub := strArg(args, 1, ctx)
	return Sequence{BoolItem(strings.Contains(s, sub))}, nil
}

func fnStartsWith(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	pre := strArg(args, 1, ctx)
	return Sequence{BoolItem(strings.HasPrefix(s, pre))}, nil
}

func fnEndsWith(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	suf := strArg(args, 1, ctx)
	return Sequence{BoolItem(strings.HasSuffix(s, suf))}, nil
}

func fnSubstring(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	// XPath uses 1-based, and rounds fractional positions.
	startF := numArg(args, 1)
	runes := []rune(s)
	total := len(runes)

	startIdx := int(math.Round(startF)) - 1
	if len(args) >= 3 {
		lenF := numArg(args, 2)
		endIdx := int(math.Round(startF+lenF)) - 1
		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx > total {
			endIdx = total
		}
		if startIdx >= endIdx {
			return Sequence{StringItem("")}, nil
		}
		return Sequence{StringItem(string(runes[startIdx:endIdx]))}, nil
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= total {
		return Sequence{StringItem("")}, nil
	}
	return Sequence{StringItem(string(runes[startIdx:]))}, nil
}

func fnSubstringBefore(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	sub := strArg(args, 1, ctx)
	if sub == "" {
		return Sequence{StringItem("")}, nil
	}
	idx := strings.Index(s, sub)
	if idx < 0 {
		return Sequence{StringItem("")}, nil
	}
	return Sequence{StringItem(s[:idx])}, nil
}

func fnSubstringAfter(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	sub := strArg(args, 1, ctx)
	if sub == "" {
		return Sequence{StringItem(s)}, nil
	}
	idx := strings.Index(s, sub)
	if idx < 0 {
		return Sequence{StringItem("")}, nil
	}
	return Sequence{StringItem(s[idx+len(sub):])}, nil
}

func fnStringLength(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	return Sequence{IntegerItem(int64(utf8.RuneCountInString(s)))}, nil
}

func fnNormalizeSpace(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	fields := strings.Fields(s)
	return Sequence{StringItem(strings.Join(fields, " "))}, nil
}

func fnUpperCase(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem(strings.ToUpper(strArg(args, 0, ctx)))}, nil
}

func fnLowerCase(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem(strings.ToLower(strArg(args, 0, ctx)))}, nil
}

func fnTranslate(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	from := []rune(strArg(args, 1, ctx))
	to := []rune(strArg(args, 2, ctx))
	var sb strings.Builder
	for _, r := range s {
		found := false
		for i, f := range from {
			if r == f {
				found = true
				if i < len(to) {
					sb.WriteRune(to[i])
				}
				break
			}
		}
		if !found {
			sb.WriteRune(r)
		}
	}
	return Sequence{StringItem(sb.String())}, nil
}

func fnReplace(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	pat := strArg(args, 1, ctx)
	repl := strArg(args, 2, ctx)
	flags := strArg(args, 3, ctx)
	re, err := compileRegex(pat, flags)
	if err != nil {
		return nil, err
	}
	// Convert XPath $1 replacement syntax to Go $1.
	goRepl := xpathReplToGo(repl)
	return Sequence{StringItem(re.ReplaceAllString(s, goRepl))}, nil
}

func fnMatches(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	pat := strArg(args, 1, ctx)
	flags := strArg(args, 2, ctx)
	re, err := compileRegex(pat, flags)
	if err != nil {
		return nil, err
	}
	return Sequence{BoolItem(re.MatchString(s))}, nil
}

func fnTokenize(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	pat := strArg(args, 1, ctx)
	flags := strArg(args, 2, ctx)
	re, err := compileRegex(pat, flags)
	if err != nil {
		return nil, err
	}
	parts := re.Split(s, -1)
	seq := make(Sequence, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			seq = append(seq, StringItem(p))
		}
	}
	return seq, nil
}

func fnCompare(ctx *Context, args []Sequence) (Sequence, error) {
	a := strArg(args, 0, ctx)
	b := strArg(args, 1, ctx)
	if a < b {
		return Sequence{IntegerItem(-1)}, nil
	}
	if a > b {
		return Sequence{IntegerItem(1)}, nil
	}
	return Sequence{IntegerItem(0)}, nil
}

func fnCodepointsToString(ctx *Context, args []Sequence) (Sequence, error) {
	var sb strings.Builder
	if len(args) > 0 {
		for _, item := range args[0] {
			f, _ := TypedNumber(item)
			sb.WriteRune(rune(int(f)))
		}
	}
	return Sequence{StringItem(sb.String())}, nil
}

func fnStringToCodepoints(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	var seq Sequence
	for _, r := range s {
		seq = append(seq, IntegerItem(int64(r)))
	}
	return seq, nil
}

func fnEncodeForURI(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	var sb strings.Builder
	for _, b := range []byte(s) {
		if isURIUnreserved(b) {
			sb.WriteByte(b)
		} else {
			sb.WriteString(fmt.Sprintf("%%%02X", b))
		}
	}
	return Sequence{StringItem(sb.String())}, nil
}

func isURIUnreserved(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') || b == '-' || b == '_' || b == '.' || b == '~'
}

func fnNormalizeUnicode(ctx *Context, args []Sequence) (Sequence, error) {
	s := strArg(args, 0, ctx)
	return Sequence{StringItem(s)}, nil // NFC is default Go string representation
}

// ---- Boolean ----

func fnNot(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{BoolItem(true)}, nil
	}
	return Sequence{BoolItem(!BooleanValue(args[0]))}, nil
}

func fnTrue(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{BoolItem(true)}, nil
}

func fnFalse(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{BoolItem(false)}, nil
}

// ---- Numeric ----

func fnAbs(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{DoubleItem(math.Abs(numArg(args, 0)))}, nil
}

func fnFloor(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{DoubleItem(math.Floor(numArg(args, 0)))}, nil
}

func fnCeiling(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{DoubleItem(math.Ceil(numArg(args, 0)))}, nil
}

func fnRound(ctx *Context, args []Sequence) (Sequence, error) {
	f := numArg(args, 0)
	return Sequence{DoubleItem(math.Round(f))}, nil
}

func fnRoundHalfToEven(ctx *Context, args []Sequence) (Sequence, error) {
	f := numArg(args, 0)
	scale := 0
	if len(args) >= 2 {
		scale = int(numArg(args, 1))
	}
	if scale == 0 {
		return Sequence{DoubleItem(math.RoundToEven(f))}, nil
	}
	factor := math.Pow(10, float64(scale))
	return Sequence{DoubleItem(math.RoundToEven(f*factor) / factor)}, nil
}

// ---- Aggregate ----

func fnCount(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{IntegerItem(0)}, nil
	}
	return Sequence{IntegerItem(int64(len(args[0])))}, nil
}

func fnSum(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		if len(args) >= 2 {
			return args[1], nil
		}
		return Sequence{IntegerItem(0)}, nil
	}
	sum := 0.0
	for _, item := range args[0] {
		f, _ := TypedNumber(item)
		sum += f
	}
	return Sequence{DoubleItem(sum)}, nil
}

func fnAvg(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return Sequence{}, nil
	}
	sum := 0.0
	for _, item := range args[0] {
		f, _ := TypedNumber(item)
		sum += f
	}
	return Sequence{DoubleItem(sum / float64(len(args[0])))}, nil
}

func fnMin(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return Sequence{}, nil
	}
	min := math.Inf(1)
	for _, item := range args[0] {
		f, _ := TypedNumber(item)
		if f < min {
			min = f
		}
	}
	return Sequence{DoubleItem(min)}, nil
}

func fnMax(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return Sequence{}, nil
	}
	max := math.Inf(-1)
	for _, item := range args[0] {
		f, _ := TypedNumber(item)
		if f > max {
			max = f
		}
	}
	return Sequence{DoubleItem(max)}, nil
}

func fnPosition(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{IntegerItem(int64(ctx.Position))}, nil
}

func fnLast(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{IntegerItem(int64(ctx.Size))}, nil
}

func fnEmpty(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{BoolItem(true)}, nil
	}
	return Sequence{BoolItem(len(args[0]) == 0)}, nil
}

func fnExists(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{BoolItem(false)}, nil
	}
	return Sequence{BoolItem(len(args[0]) > 0)}, nil
}

func fnDistinctValues(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{}, nil
	}
	seen := make(map[string]bool)
	var out Sequence
	for _, item := range args[0] {
		s := item.StringValue()
		if !seen[s] {
			seen[s] = true
			out = append(out, item)
		}
	}
	return out, nil
}

func fnIndexOf(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) < 2 {
		return Sequence{}, nil
	}
	search := args[1]
	var out Sequence
	for i, item := range args[0] {
		match := false
		for _, s := range search {
			cmp, _ := CompareItems(item, s)
			if cmp == 0 {
				match = true
				break
			}
		}
		if match {
			out = append(out, IntegerItem(int64(i+1)))
		}
	}
	return out, nil
}

func fnInsertBefore(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) < 3 {
		return Sequence{}, nil
	}
	seq := args[0]
	pos := int(NumberValue(args[1])) - 1
	ins := args[2]
	if pos < 0 {
		pos = 0
	}
	if pos > len(seq) {
		pos = len(seq)
	}
	var out Sequence
	out = append(out, seq[:pos]...)
	out = append(out, ins...)
	out = append(out, seq[pos:]...)
	return out, nil
}

func fnRemove(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) < 2 {
		return Sequence{}, nil
	}
	seq := args[0]
	pos := int(NumberValue(args[1])) - 1
	if pos < 0 || pos >= len(seq) {
		return seq, nil
	}
	var out Sequence
	out = append(out, seq[:pos]...)
	out = append(out, seq[pos+1:]...)
	return out, nil
}

func fnReverse(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{}, nil
	}
	seq := args[0]
	out := make(Sequence, len(seq))
	for i, item := range seq {
		out[len(seq)-1-i] = item
	}
	return out, nil
}

func fnSubsequence(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) < 2 {
		return Sequence{}, nil
	}
	seq := args[0]
	start := int(math.Round(NumberValue(args[1]))) - 1
	if start < 0 {
		start = 0
	}
	if start >= len(seq) {
		return Sequence{}, nil
	}
	if len(args) >= 3 {
		length := int(math.Round(NumberValue(args[2])))
		end := start + length
		if end > len(seq) {
			end = len(seq)
		}
		return seq[start:end], nil
	}
	return seq[start:], nil
}

func fnUnordered(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{}, nil
	}
	return args[0], nil
}

func fnZeroOrOne(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		return Sequence{}, nil
	}
	if len(args[0]) > 1 {
		return nil, fmt.Errorf("zero-or-one: sequence has more than one item")
	}
	return args[0], nil
}

func fnOneOrMore(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, fmt.Errorf("one-or-more: empty sequence")
	}
	return args[0], nil
}

func fnExactlyOne(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 || len(args[0]) != 1 {
		return nil, fmt.Errorf("exactly-one: sequence does not have exactly one item")
	}
	return args[0], nil
}

func fnData(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 {
		if ctx.Item != nil {
			return Atomize(Sequence{ctx.Item}), nil
		}
		return Sequence{}, nil
	}
	return Atomize(args[0]), nil
}

func fnDeepEqual(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) < 2 {
		return Sequence{BoolItem(false)}, nil
	}
	return Sequence{BoolItem(deepEqual(args[0], args[1]))}, nil
}

func deepEqual(a, b Sequence) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].StringValue() != b[i].StringValue() {
			return false
		}
	}
	return true
}

// ---- Node functions ----

func fnName(ctx *Context, args []Sequence) (Sequence, error) {
	n := contextOrArgNode(ctx, args)
	if n == nil {
		return Sequence{StringItem("")}, nil
	}
	return Sequence{StringItem(n.QName())}, nil
}

func fnLocalName(ctx *Context, args []Sequence) (Sequence, error) {
	n := contextOrArgNode(ctx, args)
	if n == nil {
		return Sequence{StringItem("")}, nil
	}
	return Sequence{StringItem(n.LocalName)}, nil
}

func fnNamespaceURI(ctx *Context, args []Sequence) (Sequence, error) {
	n := contextOrArgNode(ctx, args)
	if n == nil {
		return Sequence{StringItem("")}, nil
	}
	return Sequence{StringItem(n.NamespaceURI)}, nil
}

func fnNodeName(ctx *Context, args []Sequence) (Sequence, error) {
	n := contextOrArgNode(ctx, args)
	if n == nil {
		return Sequence{}, nil
	}
	return Sequence{StringItem(n.QName())}, nil
}

func fnNilled(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{BoolItem(false)}, nil
}

func fnBaseURI(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem("")}, nil
}

func fnDocumentURI(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem("")}, nil
}

func fnRoot(ctx *Context, args []Sequence) (Sequence, error) {
	n := contextOrArgNode(ctx, args)
	if n == nil {
		return Sequence{}, nil
	}
	// Walk up to document node.
	for n.Parent != nil {
		n = n.Parent
	}
	return Sequence{&NodeItem{Node: n}}, nil
}

func fnLang(ctx *Context, args []Sequence) (Sequence, error) {
	testLang := strArg(args, 0, ctx)
	n := ctx.CurrentNode()
	for n != nil {
		v, ok := n.AttrNS("http://www.w3.org/XML/1998/namespace", "lang")
		if ok {
			return Sequence{BoolItem(strings.EqualFold(v, testLang) || strings.HasPrefix(strings.ToLower(v), strings.ToLower(testLang)+"-"))}, nil
		}
		n = n.Parent
	}
	return Sequence{BoolItem(false)}, nil
}

func fnId(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) == 0 || ctx.Doc == nil {
		return Sequence{}, nil
	}
	ids := strings.Fields(StringValue(args[0]))
	var out Sequence
	for _, id := range ids {
		found := findById(ctx.Doc.Root, id)
		if found != nil {
			out = append(out, &NodeItem{Node: found})
		}
	}
	return out, nil
}

func findById(n *dom.Node, id string) *dom.Node {
	if n.Type == dom.NodeElement {
		for _, a := range n.Attributes {
			if a.Value == id && (strings.ToLower(a.LocalName) == "id") {
				return n
			}
		}
	}
	for _, c := range n.Children {
		if r := findById(c, id); r != nil {
			return r
		}
	}
	return nil
}

func fnIdRef(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{}, nil
}

func fnGenerateId(ctx *Context, args []Sequence) (Sequence, error) {
	n := contextOrArgNode(ctx, args)
	if n == nil {
		return Sequence{StringItem("")}, nil
	}
	return Sequence{StringItem(fmt.Sprintf("id%d", n.DocOrder))}, nil
}

func contextOrArgNode(ctx *Context, args []Sequence) *dom.Node {
	if len(args) == 0 {
		return ctx.CurrentNode()
	}
	if len(args[0]) > 0 && args[0][0].IsNode() {
		return args[0][0].(*NodeItem).Node
	}
	return nil
}

// ---- Date/time ----

func fnCurrentDateTime(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem(time.Now().Format(time.RFC3339))}, nil
}

func fnCurrentDate(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem(time.Now().Format("2006-01-02"))}, nil
}

func fnCurrentTime(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem(time.Now().Format("15:04:05"))}, nil
}

func fnImplicitTimezone(ctx *Context, args []Sequence) (Sequence, error) {
	_, offset := time.Now().Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	sign := "+"
	if hours < 0 {
		sign = "-"
		hours = -hours
		minutes = -minutes
	}
	return Sequence{StringItem(fmt.Sprintf("%s%02d:%02d", sign, hours, minutes))}, nil
}

// ---- Misc ----

func fnError(ctx *Context, args []Sequence) (Sequence, error) {
	msg := "error() called"
	if len(args) >= 2 {
		msg = StringValue(args[1])
	}
	return nil, fmt.Errorf("xslt: %s", msg)
}

func fnTrace(ctx *Context, args []Sequence) (Sequence, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return Sequence{}, nil
}

func fnStaticBaseURI(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{StringItem("")}, nil
}

func fnFunctionAvailable(ctx *Context, args []Sequence) (Sequence, error) {
	name := strArg(args, 0, ctx)
	_, ok := ctx.Funcs[name]
	return Sequence{BoolItem(ok)}, nil
}

func fnElementAvailable(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{BoolItem(false)}, nil
}

func fnTypeAvailable(ctx *Context, args []Sequence) (Sequence, error) {
	return Sequence{BoolItem(false)}, nil
}

func fnSystemProperty(ctx *Context, args []Sequence) (Sequence, error) {
	name := strArg(args, 0, ctx)
	switch name {
	case "xsl:version":
		return Sequence{StringItem("2.0")}, nil
	case "xsl:vendor":
		return Sequence{StringItem("xslt2-go")}, nil
	case "xsl:vendor-url":
		return Sequence{StringItem("https://github.com")}, nil
	}
	return Sequence{StringItem("")}, nil
}

// ---- Regex helpers ----

func compileRegex(pat, flags string) (*regexp.Regexp, error) {
	// Convert XPath regex flags to Go.
	goFlags := "(?m)"
	if strings.Contains(flags, "s") {
		goFlags = "(?ms)" // dot-all mode
	}
	if strings.Contains(flags, "i") {
		goFlags = goFlags[:len(goFlags)-1] + "i)"
	}
	return regexp.Compile(goFlags + pat)
}

func xpathReplToGo(repl string) string {
	// XPath uses $0, $1 ... Go uses $0, $1 ... same syntax — pass through.
	return repl
}

// ---- Sort helper ----

func sortSequence(seq Sequence, key func(Item) string, order string) Sequence {
	type kv struct {
		k string
		v Item
	}
	pairs := make([]kv, len(seq))
	for i, item := range seq {
		pairs[i] = kv{key(item), item}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if order == "descending" {
			return pairs[i].k > pairs[j].k
		}
		return pairs[i].k < pairs[j].k
	})
	out := make(Sequence, len(seq))
	for i, p := range pairs {
		out[i] = p.v
	}
	return out
}
