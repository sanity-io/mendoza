package mendoza

import (
	"bytes"
	"unicode/utf16"
	"unicode/utf8"
)

// Scanning primitives for RawPatcher. They work on spans of a byte slice and
// never decode a value they only need to skip. They assume well formed JSON:
// malformed input makes them return -1 or a span that the patcher later
// rejects, but they never index out of range.

var whitespace = [256]bool{' ': true, '\t': true, '\n': true, '\r': true}

// scalarEnd marks the bytes that end a number or literal.
var scalarEnd = [256]bool{
	' ': true, '\t': true, '\n': true, '\r': true,
	',': true, ':': true, '}': true, ']': true,
}

// structural marks the bytes skipContainer has to look at.
var structural = [256]bool{'"': true, '{': true, '}': true, '[': true, ']': true}

func skipWS(b []byte, i int) int {
	for i < len(b) && whitespace[b[i]] {
		i++
	}
	return i
}

// trimEnd returns the end of [start, end) without trailing whitespace.
func trimEnd(b []byte, start, end int) int {
	for end > start && whitespace[b[end-1]] {
		end--
	}
	return end
}

// skipString returns the index after the string starting at b[i] == '"',
// and whether it contains escapes; -1 if it is not terminated.
func skipString(b []byte, i int) (int, bool) {
	esc := false
	i++
	for i <= len(b) {
		q := bytes.IndexByte(b[i:], '"')
		if q < 0 {
			return -1, esc
		}
		bs := bytes.IndexByte(b[i:i+q], '\\')
		if bs < 0 {
			return i + q + 1, esc
		}
		esc = true
		i += bs + 2 // the backslash and the byte it escapes
	}
	return -1, esc
}

// skipValue returns the index after the value starting at b[i], or -1.
func skipValue(b []byte, i int) int {
	if i >= len(b) {
		return -1
	}
	switch c := b[i]; c {
	case '"':
		e, _ := skipString(b, i)
		return e
	case '{', '[':
		return skipContainer(b, i)
	case 't', 'f', 'n', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		j := i + 1
		for j < len(b) && !scalarEnd[b[j]] {
			j++
		}
		return j
	default:
		return -1
	}
}

// skipContainer returns the index after the object or array at b[i]. It
// balances brackets and skips strings, without checking the grammar.
func skipContainer(b []byte, i int) int {
	depth := 0
	for i < len(b) {
		c := b[i]
		if !structural[c] {
			i++
			continue
		}
		switch c {
		case '"':
			e, _ := skipString(b, i)
			if e < 0 {
				return -1
			}
			i = e
			continue
		case '{', '[':
			depth++
		default: // '}', ']'
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		i++
	}
	return -1
}

// unescape appends the decoded content of a JSON string (without quotes) to
// dst. Invalid escapes are copied as is; lone surrogates become U+FFFD, as in
// encoding/json.
func unescape(dst, s []byte) []byte {
	for i := 0; i < len(s); {
		j := bytes.IndexByte(s[i:], '\\')
		if j < 0 {
			return append(dst, s[i:]...)
		}
		dst = append(dst, s[i:i+j]...)
		i += j
		if i+1 >= len(s) {
			return append(dst, s[i:]...)
		}
		switch c := s[i+1]; c {
		case '"', '\\', '/':
			dst = append(dst, c)
			i += 2
		case 'b':
			dst = append(dst, '\b')
			i += 2
		case 'f':
			dst = append(dst, '\f')
			i += 2
		case 'n':
			dst = append(dst, '\n')
			i += 2
		case 'r':
			dst = append(dst, '\r')
			i += 2
		case 't':
			dst = append(dst, '\t')
			i += 2
		case 'u':
			r, ok := hex4(s, i+2)
			if !ok {
				dst = append(dst, s[i])
				i++
				continue
			}
			i += 6
			if utf16.IsSurrogate(r) {
				r2, ok := rune(0), false
				if i+1 < len(s) && s[i] == '\\' && s[i+1] == 'u' {
					r2, ok = hex4(s, i+2)
				}
				if dec := utf16.DecodeRune(r, r2); ok && dec != utf8.RuneError {
					r = dec
					i += 6
				} else {
					r = utf8.RuneError
				}
			}
			var buf [utf8.UTFMax]byte
			n := utf8.EncodeRune(buf[:], r)
			dst = append(dst, buf[:n]...)
		default:
			dst = append(dst, s[i])
			i++
		}
	}
	return dst
}

func hex4(s []byte, i int) (rune, bool) {
	if i+4 > len(s) {
		return 0, false
	}
	var r rune
	for _, c := range s[i : i+4] {
		switch {
		case '0' <= c && c <= '9':
			c -= '0'
		case 'a' <= c && c <= 'f':
			c = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			c = c - 'A' + 10
		default:
			return 0, false
		}
		r = r*16 + rune(c)
	}
	return r, true
}

const hexDigits = "0123456789abcdef"

// escape appends s as JSON string content (without quotes) to dst. Only
// what JSON requires is escaped; other bytes, including split UTF-8
// sequences, are copied so that adjacent pieces can complete them.
func escape(dst, s []byte) []byte {
	start := 0
	for i, c := range s {
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		dst = append(dst, s[start:i]...)
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		start = i + 1
	}
	return append(dst, s[start:]...)
}
