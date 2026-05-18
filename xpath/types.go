package xpath

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/davewins/xslt/dom"
)

// Item is any XPath 2.0 item (node or atomic value).
type Item interface {
	StringValue() string
	IsNode() bool
}

// Sequence is an ordered list of items.
type Sequence []Item

// NodeItem wraps a DOM node as an XPath item.
type NodeItem struct {
	Node *dom.Node
}

func (n *NodeItem) StringValue() string { return n.Node.StringValue() }
func (n *NodeItem) IsNode() bool        { return true }

// AtomicKind classifies an atomic value type.
type AtomicKind int

const (
	KindString AtomicKind = iota
	KindInteger
	KindDecimal
	KindDouble
	KindBoolean
	KindDate
	KindDateTime
	KindTime
	KindAnyURI
	KindQName
	KindUntypedAtomic
)

// AtomicValue holds an XPath 2.0 atomic value.
type AtomicValue struct {
	Kind  AtomicKind
	Str   string
	Num   float64
	Bool  bool
}

func (a *AtomicValue) IsNode() bool { return false }

func (a *AtomicValue) StringValue() string {
	switch a.Kind {
	case KindBoolean:
		if a.Bool {
			return "true"
		}
		return "false"
	case KindInteger:
		return strconv.FormatInt(int64(a.Num), 10)
	case KindDecimal, KindDouble:
		if math.IsNaN(a.Num) {
			return "NaN"
		}
		if math.IsInf(a.Num, 1) {
			return "INF"
		}
		if math.IsInf(a.Num, -1) {
			return "-INF"
		}
		// Format without trailing zeros for integer-valued doubles.
		if a.Num == math.Trunc(a.Num) && !math.IsInf(a.Num, 0) {
			return strconv.FormatFloat(a.Num, 'f', 0, 64)
		}
		return strconv.FormatFloat(a.Num, 'g', -1, 64)
	default:
		return a.Str
	}
}

// Constructors

func StringItem(s string) Item {
	return &AtomicValue{Kind: KindString, Str: s}
}

func IntegerItem(n int64) Item {
	return &AtomicValue{Kind: KindInteger, Num: float64(n)}
}

func DoubleItem(f float64) Item {
	return &AtomicValue{Kind: KindDouble, Num: f}
}

func DecimalItem(f float64) Item {
	return &AtomicValue{Kind: KindDecimal, Num: f}
}

func BoolItem(b bool) Item {
	return &AtomicValue{Kind: KindBoolean, Bool: b}
}

// Predicates

func IsTrue(b bool) Item      { return BoolItem(b) }
func IsString(i Item) bool    { return !i.IsNode() && i.(*AtomicValue).Kind == KindString }
func IsInteger(i Item) bool   { return !i.IsNode() && i.(*AtomicValue).Kind == KindInteger }
func IsDouble(i Item) bool    { return !i.IsNode() && i.(*AtomicValue).Kind == KindDouble }
func IsBoolean(i Item) bool   { return !i.IsNode() && i.(*AtomicValue).Kind == KindBoolean }

// TypedNumber returns the numeric value of an item.
func TypedNumber(i Item) (float64, error) {
	switch v := i.(type) {
	case *NodeItem:
		return strToNumber(v.Node.StringValue())
	case *AtomicValue:
		switch v.Kind {
		case KindInteger, KindDecimal, KindDouble:
			return v.Num, nil
		case KindBoolean:
			if v.Bool {
				return 1, nil
			}
			return 0, nil
		default:
			return strToNumber(v.Str)
		}
	}
	return math.NaN(), nil
}

func strToNumber(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return math.NaN(), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN(), nil
	}
	return f, nil
}

// BooleanValue computes the effective boolean value of a sequence.
func BooleanValue(seq Sequence) bool {
	if len(seq) == 0 {
		return false
	}
	first := seq[0]
	if first.IsNode() {
		return true
	}
	av := first.(*AtomicValue)
	switch av.Kind {
	case KindBoolean:
		return av.Bool
	case KindString, KindAnyURI, KindUntypedAtomic:
		return av.Str != ""
	case KindInteger, KindDecimal, KindDouble:
		return av.Num != 0 && !math.IsNaN(av.Num)
	}
	return len(seq) > 0
}

// StringValue returns the string value of the first item in a sequence, or "".
func StringValue(seq Sequence) string {
	if len(seq) == 0 {
		return ""
	}
	return seq[0].StringValue()
}

// NumberValue returns the numeric value of the first item in a sequence.
func NumberValue(seq Sequence) float64 {
	if len(seq) == 0 {
		return math.NaN()
	}
	f, _ := TypedNumber(seq[0])
	return f
}

// CompareItems compares two items using value comparison.
// Returns -1, 0, or 1. For general comparison, the caller iterates.
func CompareItems(a, b Item) (int, error) {
	an, aErr := TypedNumber(a)
	bn, bErr := TypedNumber(b)
	if aErr == nil && bErr == nil && !math.IsNaN(an) && !math.IsNaN(bn) {
		if an < bn {
			return -1, nil
		}
		if an > bn {
			return 1, nil
		}
		return 0, nil
	}
	as := a.StringValue()
	bs := b.StringValue()
	if as < bs {
		return -1, nil
	}
	if as > bs {
		return 1, nil
	}
	return 0, nil
}

// GeneralCompare performs a general comparison (=, !=, <, <=, >, >=).
func GeneralCompare(left, right Sequence, op string) bool {
	for _, l := range left {
		for _, r := range right {
			cmp, _ := CompareItems(l, r)
			switch op {
			case "=":
				if cmp == 0 {
					return true
				}
			case "!=":
				if cmp != 0 {
					return true
				}
			case "<":
				if cmp < 0 {
					return true
				}
			case "<=":
				if cmp <= 0 {
					return true
				}
			case ">":
				if cmp > 0 {
					return true
				}
			case ">=":
				if cmp >= 0 {
					return true
				}
			}
		}
	}
	return false
}

// ValueCompare performs a value comparison (eq, ne, lt, le, gt, ge).
// Returns an error if either sequence has cardinality != 1.
func ValueCompare(left, right Sequence, op string) (bool, error) {
	if len(left) == 0 || len(right) == 0 {
		return false, nil // empty sequence → false (not an error)
	}
	if len(left) > 1 || len(right) > 1 {
		return false, fmt.Errorf("value comparison requires single items")
	}
	cmp, err := CompareItems(left[0], right[0])
	if err != nil {
		return false, err
	}
	switch op {
	case "eq":
		return cmp == 0, nil
	case "ne":
		return cmp != 0, nil
	case "lt":
		return cmp < 0, nil
	case "le":
		return cmp <= 0, nil
	case "gt":
		return cmp > 0, nil
	case "ge":
		return cmp >= 0, nil
	}
	return false, fmt.Errorf("unknown value comparison op: %s", op)
}

// Atomize converts a sequence to atomic values (extracts typed values from nodes).
func Atomize(seq Sequence) Sequence {
	var out Sequence
	for _, item := range seq {
		if item.IsNode() {
			out = append(out, StringItem(item.StringValue()))
		} else {
			out = append(out, item)
		}
	}
	return out
}

// NodeOrder sorts a slice of NodeItems into document order.
func NodeOrder(nodes []*dom.Node) []*dom.Node {
	// Simple insertion sort by DocOrder (usually small sequences).
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && nodes[j].DocOrder < nodes[j-1].DocOrder; j-- {
			nodes[j], nodes[j-1] = nodes[j-1], nodes[j]
		}
	}
	return nodes
}

// SeqToNodes extracts DOM nodes from a sequence.
func SeqToNodes(seq Sequence) []*dom.Node {
	var out []*dom.Node
	for _, item := range seq {
		if item.IsNode() {
			out = append(out, item.(*NodeItem).Node)
		}
	}
	return out
}

// NodesToSeq wraps DOM nodes as a sequence.
func NodesToSeq(nodes []*dom.Node) Sequence {
	seq := make(Sequence, len(nodes))
	for i, n := range nodes {
		seq[i] = &NodeItem{Node: n}
	}
	return seq
}
