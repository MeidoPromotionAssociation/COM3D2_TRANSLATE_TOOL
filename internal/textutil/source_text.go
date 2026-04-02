package textutil

import (
	"slices"
	"strings"
	"unicode"
)

var sourceTextCleanupRunes = []rune{
	'\u0009',
	'\u000A',
	'\u000B',
	'\u000C',
	'\u000D',
	'\u0020',
	'\u00A0',
	'\u180E',
	'\u200B',
	'\u200C',
	'\u200D',
	'\u2060',
	'\u3000',
	'\uFEFF',
}

func SourceTextCleanupRunes() []rune {
	return slices.Clone(sourceTextCleanupRunes)
}

func NormalizeSourceText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.Map(func(r rune) rune {
		if isIgnoredSourceTextRune(r) {
			return -1
		}
		return r
	}, value)
	return strings.TrimFunc(value, isTrimmedSourceTextRune)
}

func IsBlankSourceText(value string) bool {
	return NormalizeSourceText(value) == ""
}

func isIgnoredSourceTextRune(r rune) bool {
	switch r {
	case '\u180E', '\u200B', '\u200C', '\u200D', '\u2060', '\uFEFF':
		return true
	default:
		return false
	}
}

func isTrimmedSourceTextRune(r rune) bool {
	return unicode.IsSpace(r) || isIgnoredSourceTextRune(r)
}
