package xpath

// TokenKind classifies an XPath lexical token.
type TokenKind int

const (
	TokEOF TokenKind = iota
	TokInteger
	TokDecimal
	TokDouble
	TokString
	TokName     // NCName or QName
	TokPlus     // +
	TokMinus    // -
	TokStar     // *  (multiply or wildcard)
	TokSlash    // /
	TokDSlash   // //
	TokPipe     // |
	TokComma    // ,
	TokDot      // .
	TokDDot     // ..
	TokAt       // @
	TokColon    // :
	TokDColon   // ::
	TokLParen   // (
	TokRParen   // )
	TokLBrack   // [
	TokRBrack   // ]
	TokLBrace   // {
	TokRBrace   // }
	TokQuestion // ?
	TokEq       // =
	TokNE       // !=
	TokLT       // <
	TokLE       // <=
	TokGT       // >
	TokGE       // >=
	TokDLT      // << (node precedes)
	TokDGT      // >> (node follows)
	TokDollar   // $
	TokTo       // to  (range)
	TokReturn   // return (for expression)
	TokIn       // in
	TokSatisfies // satisfies
	TokThen     // then
	TokElse     // else
	TokAnd      // and
	TokOr       // or
	TokDiv      // div
	TokIDiv     // idiv
	TokMod      // mod
	TokEqOp     // eq
	TokNeOp     // ne
	TokLtOp     // lt
	TokLeOp     // le
	TokGtOp     // gt
	TokGeOp     // ge
	TokIs       // is
	TokFor      // for
	TokSome     // some
	TokEvery    // every
	TokIf       // if
	TokUnion    // union
	TokIntersect // intersect
	TokExcept   // except
	TokInstance // instance
	TokOf       // of
	TokTreat    // treat
	TokAs       // as
	TokCastable // castable
	TokCast     // cast
	TokAxisSep  // used internally after axis name + "::"
)

// Token is a single lexical token.
type Token struct {
	Kind  TokenKind
	Value string // raw text of the token
	Pos   int    // byte offset in the expression string
}

var keywords = map[string]TokenKind{
	"for":       TokFor,
	"some":      TokSome,
	"every":     TokEvery,
	"in":        TokIn,
	"return":    TokReturn,
	"satisfies": TokSatisfies,
	"if":        TokIf,
	"then":      TokThen,
	"else":      TokElse,
	"and":       TokAnd,
	"or":        TokOr,
	"div":       TokDiv,
	"idiv":      TokIDiv,
	"mod":       TokMod,
	"eq":        TokEqOp,
	"ne":        TokNeOp,
	"lt":        TokLtOp,
	"le":        TokLeOp,
	"gt":        TokGtOp,
	"ge":        TokGeOp,
	"is":        TokIs,
	"to":        TokTo,
	"union":     TokUnion,
	"intersect": TokIntersect,
	"except":    TokExcept,
	"instance":  TokInstance,
	"of":        TokOf,
	"treat":     TokTreat,
	"as":        TokAs,
	"castable":  TokCastable,
	"cast":      TokCast,
}
