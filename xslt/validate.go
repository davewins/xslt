package xslt

import (
	"fmt"
	"strings"
	"unicode"
)

// isNameStart reports whether r may start an XML Name (approximation of the XML
// 1.0 NameStartChar production, sufficient to reject injection characters).
func isNameStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

// isNameChar reports whether r may appear after the first character of an XML Name.
func isNameChar(r rune) bool {
	return isNameStart(r) || r == '-' || r == '.' || unicode.IsDigit(r) ||
		unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r)
}

// validateNCName verifies s is a non-empty XML NCName (a Name with no colon).
func validateNCName(s string) error {
	if s == "" {
		return fmt.Errorf("empty name")
	}
	for i, r := range s {
		if i == 0 {
			if !isNameStart(r) {
				return fmt.Errorf("invalid start character %q in name %q", r, s)
			}
			continue
		}
		if !isNameChar(r) || r == ':' {
			return fmt.Errorf("invalid character %q in name %q", r, s)
		}
	}
	return nil
}

// validateQName verifies s is a valid XML QName: an NCName, optionally prefixed
// by another NCName and a single colon. This rejects computed element/attribute
// names that would otherwise inject markup when serialized.
func validateQName(s string) error {
	if s == "" {
		return fmt.Errorf("empty qualified name")
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		if err := validateNCName(s[:i]); err != nil {
			return fmt.Errorf("invalid prefix in %q: %w", s, err)
		}
		return validateNCName(s[i+1:])
	}
	return validateNCName(s)
}

// validateCommentText rejects comment content that cannot be serialized safely:
// XML comments may not contain "--" or end with "-", either of which would let
// untrusted data break out of the comment delimiters.
func validateCommentText(s string) error {
	if strings.Contains(s, "--") {
		return fmt.Errorf("comment content may not contain %q", "--")
	}
	if strings.HasSuffix(s, "-") {
		return fmt.Errorf("comment content may not end with %q", "-")
	}
	return nil
}

// validatePIData rejects processing-instruction data containing "?>", which
// would otherwise let untrusted data break out of the PI.
func validatePIData(s string) error {
	if strings.Contains(s, "?>") {
		return fmt.Errorf("processing-instruction data may not contain %q", "?>")
	}
	return nil
}
