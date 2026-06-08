package variables

import "strings"

// Font is a selectable Unicode pseudo-font that transforms ASCII letters and
// digits into stylistic variants. "normal" is the identity transform.
type Font struct {
	Key string
	FA  string
	EN  string
	fn  func(string) string
}

// Apply transforms s using the font identified by key. Unknown/empty keys and
// the "normal" key return s unchanged.
func Apply(key, s string) string {
	if key == "" || key == "normal" {
		return s
	}
	if f, ok := fontIndex[key]; ok {
		return f.fn(s)
	}
	return s
}

// Fonts returns all fonts in display order (normal first).
func Fonts() []Font { return fontList }

// FontName returns the localized display name of a font key.
func FontName(key, lang string) string {
	f, ok := fontIndex[key]
	if !ok {
		f = fontList[0]
	}
	if lang == "en" {
		return f.EN
	}
	return f.FA
}

// mapper builds a transform from contiguous base code points plus an exceptions
// table that patches the "letterlike" holes in the Unicode math blocks. When
// fold is true, ASCII uppercase is folded to lowercase before mapping (used by
// small caps, which has a single glyph per letter).
func mapper(upper, lower, digit rune, except map[rune]rune, fold bool) func(string) string {
	return func(s string) string {
		var b strings.Builder
		b.Grow(len(s) * 2)
		for _, r := range s {
			if e, ok := except[r]; ok {
				b.WriteRune(e)
				continue
			}
			switch {
			case fold && r >= 'A' && r <= 'Z':
				if e, ok := except[r-'A'+'a']; ok {
					b.WriteRune(e)
				} else {
					b.WriteRune(r)
				}
			case r >= 'A' && r <= 'Z' && upper != 0:
				b.WriteRune(upper + (r - 'A'))
			case r >= 'a' && r <= 'z' && lower != 0:
				b.WriteRune(lower + (r - 'a'))
			case r >= '0' && r <= '9' && digit != 0:
				b.WriteRune(digit + (r - '0'))
			default:
				b.WriteRune(r)
			}
		}
		return b.String()
	}
}

// circledDigits / smallCaps are the irregular tables that can't be expressed as
// a simple offset.
var circledDigits = map[rune]rune{
	'0': 0x24EA, '1': 0x2460, '2': 0x2461, '3': 0x2462, '4': 0x2463,
	'5': 0x2464, '6': 0x2465, '7': 0x2466, '8': 0x2467, '9': 0x2468,
}

var smallCaps = map[rune]rune{
	'a': 0x1D00, 'b': 0x0299, 'c': 0x1D04, 'd': 0x1D05, 'e': 0x1D07,
	'f': 0xA730, 'g': 0x0262, 'h': 0x029C, 'i': 0x026A, 'j': 0x1D0A,
	'k': 0x1D0B, 'l': 0x029F, 'm': 0x1D0D, 'n': 0x0274, 'o': 0x1D0F,
	'p': 0x1D18, 'q': 0xA7AF, 'r': 0x0280, 's': 0xA731, 't': 0x1D1B,
	'u': 0x1D1C, 'v': 0x1D20, 'w': 0x1D21, 'y': 0x028F, 'z': 0x1D22,
}

// merge combines several exception tables into one.
func merge(maps ...map[rune]rune) map[rune]rune {
	out := map[rune]rune{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

var (
	exItalic = map[rune]rune{'h': 0x210E}
	exScript = map[rune]rune{
		'B': 0x212C, 'E': 0x2130, 'F': 0x2131, 'H': 0x210B, 'I': 0x2110,
		'L': 0x2112, 'M': 0x2133, 'R': 0x211B,
		'e': 0x212F, 'g': 0x210A, 'o': 0x2134,
	}
	exFraktur = map[rune]rune{
		'C': 0x212D, 'H': 0x210C, 'I': 0x2111, 'R': 0x211C, 'Z': 0x2128,
	}
	exDouble = map[rune]rune{
		'C': 0x2102, 'H': 0x210D, 'N': 0x2115, 'P': 0x2119,
		'Q': 0x211A, 'R': 0x211D, 'Z': 0x2124,
	}
)

var fontList = []Font{
	{Key: "normal", FA: "عادی", EN: "Normal", fn: func(s string) string { return s }},
	{Key: "bold", FA: "ضخیم", EN: "Bold", fn: mapper(0x1D400, 0x1D41A, 0x1D7CE, nil, false)},
	{Key: "italic", FA: "ایتالیک", EN: "Italic", fn: mapper(0x1D434, 0x1D44E, 0, exItalic, false)},
	{Key: "bolditalic", FA: "ضخیم ایتالیک", EN: "Bold Italic", fn: mapper(0x1D468, 0x1D482, 0x1D7CE, nil, false)},
	{Key: "script", FA: "اسکریپت", EN: "Script", fn: mapper(0x1D49C, 0x1D4B6, 0, exScript, false)},
	{Key: "fraktur", FA: "فراکتور", EN: "Fraktur", fn: mapper(0x1D504, 0x1D51E, 0, exFraktur, false)},
	{Key: "double", FA: "توخالی", EN: "Double-struck", fn: mapper(0x1D538, 0x1D552, 0x1D7D8, exDouble, false)},
	{Key: "sans", FA: "سَن‌سریف", EN: "Sans", fn: mapper(0x1D5A0, 0x1D5BA, 0x1D7E2, nil, false)},
	{Key: "sansbold", FA: "سَن ضخیم", EN: "Sans Bold", fn: mapper(0x1D5D4, 0x1D5EE, 0x1D7EC, nil, false)},
	{Key: "mono", FA: "مونو", EN: "Monospace", fn: mapper(0x1D670, 0x1D68A, 0x1D7F6, nil, false)},
	{Key: "fullwidth", FA: "تمام‌عرض", EN: "Full-width", fn: mapper(0xFF21, 0xFF41, 0xFF10, nil, false)},
	{Key: "circled", FA: "دایره‌ای", EN: "Circled", fn: mapper(0x24B6, 0x24D0, 0, circledDigits, false)},
	{Key: "smallcaps", FA: "اسمال‌کپس", EN: "Small Caps", fn: mapper(0, 0, 0, merge(smallCaps), true)},
}

var fontIndex = func() map[string]Font {
	m := make(map[string]Font, len(fontList))
	for _, f := range fontList {
		m[f.Key] = f
	}
	return m
}()
