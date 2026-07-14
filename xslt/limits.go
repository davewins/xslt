package xslt

// Limits bounds the resources a single transformation may consume, guarding
// against denial-of-service from untrusted stylesheets or source documents.
// A zero value in any field is replaced by its default (see DefaultLimits) when
// the limits are normalized, so the zero Limits{} is safe and means "defaults".
type Limits struct {
	// MaxTemplateDepth caps template/instruction recursion depth. Exceeding it
	// returns an error instead of crashing with an unrecoverable stack overflow.
	MaxTemplateDepth int
	// MaxNodeDepth caps the element nesting depth of parsed input documents.
	MaxNodeDepth int
	// MaxRangeSize caps the number of items an XPath range ("a to b") may produce.
	MaxRangeSize int
}

// Default limit values. They are generous enough for legitimate stylesheets
// (including deeply recursive ones) while preventing resource exhaustion.
const (
	defaultMaxTemplateDepth = 10000
	defaultMaxNodeDepth     = 5000
	defaultMaxRangeSize     = 10_000_000
)

// DefaultLimits returns the limits applied when a caller does not override them.
func DefaultLimits() Limits {
	return Limits{
		MaxTemplateDepth: defaultMaxTemplateDepth,
		MaxNodeDepth:     defaultMaxNodeDepth,
		MaxRangeSize:     defaultMaxRangeSize,
	}
}

// WithDefaults returns a copy of l with any unset (non-positive) field filled
// from DefaultLimits. Callers that need the effective limits before constructing
// a Processor (e.g. to size an input parse) can use this.
func (l Limits) WithDefaults() Limits {
	return l.normalize()
}

// normalize returns a copy of l with any non-positive field filled from defaults.
func (l Limits) normalize() Limits {
	d := DefaultLimits()
	if l.MaxTemplateDepth <= 0 {
		l.MaxTemplateDepth = d.MaxTemplateDepth
	}
	if l.MaxNodeDepth <= 0 {
		l.MaxNodeDepth = d.MaxNodeDepth
	}
	if l.MaxRangeSize <= 0 {
		l.MaxRangeSize = d.MaxRangeSize
	}
	return l
}
