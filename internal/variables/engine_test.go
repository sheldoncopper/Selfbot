package variables

import (
	"strings"
	"testing"
	"time"

	"github.com/selfbot/selfbot/internal/storage"
)

func TestFontApply(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"bold":      {"AB1", "𝐀𝐁𝟏"},
		"mono":      {"Ab", "𝙰𝚋"},
		"fullwidth": {"A1", "Ａ１"},
		"normal":    {"AbC 9!", "AbC 9!"},
	}
	for font, c := range cases {
		if got := Apply(font, c.in); got != c.want {
			t.Errorf("Apply(%q,%q) = %q, want %q", font, c.in, got, c.want)
		}
	}
	// Italic 'h' must use the planck-constant exception, not a missing glyph.
	if got := Apply("italic", "h"); got != "ℎ" {
		t.Errorf("italic h = %q, want ℎ", got)
	}
}

func TestGregorianToJalali(t *testing.T) {
	// 2024-03-20 is Nowruz 1403/01/01.
	jy, jm, jd := gregorianToJalali(2024, 3, 20)
	if jy != 1403 || jm != 1 || jd != 1 {
		t.Fatalf("got %d/%d/%d, want 1403/1/1", jy, jm, jd)
	}
}

func TestPredefinedCompute(t *testing.T) {
	now := time.Date(2024, 3, 20, 13, 5, 0, 0, time.UTC)

	// counter: start=10 step=5, cursor 0 -> 10, advances.
	c, _ := SpecByKey("counter")
	v, cur := c.compute(map[string]any{"start": 10, "step": 5}, 0, now)
	if v != "10" || cur != 1 {
		t.Errorf("counter step0 = %q,%d want 10,1", v, cur)
	}
	v2, _ := c.compute(map[string]any{"start": 10, "step": 5}, 1, now)
	if v2 != "15" {
		t.Errorf("counter step1 = %q want 15", v2)
	}

	// flags: cycles through provided list deterministically.
	f, _ := SpecByKey("flags")
	a, _ := f.compute(map[string]any{"items": []any{"X", "Y"}}, 0, now)
	b, _ := f.compute(map[string]any{"items": []any{"X", "Y"}}, 1, now)
	if a != "X" || b != "Y" {
		t.Errorf("flags cycle = %q,%q want X,Y", a, b)
	}

	// date jalali
	d, _ := SpecByKey("date")
	got, _ := d.compute(map[string]any{"tz": "UTC", "cal": "jalali"}, 0, now)
	if !strings.Contains(got, "1403") || !strings.Contains(got, "فروردین") {
		t.Errorf("jalali date = %q", got)
	}
}

func TestResolveValueWithFont(t *testing.T) {
	v := &storage.Variable{Type: CustomType, Config: `{"text":"hi"}`, Font: "bold"}
	got, _ := ResolveValue(v, time.Now())
	if got != "𝐡𝐢" {
		t.Errorf("custom+bold = %q want 𝐡𝐢", got)
	}
}

func TestRenderField(t *testing.T) {
	f := &storage.Field{Template: "[{x}] hi {y} end", Font: "normal"}
	vars := map[string]*storage.Variable{
		"x": {Name: "x", LastValue: "11:30"},
		// y intentionally missing -> token left verbatim
	}
	got := RenderField(f, vars)
	if got != "[11:30] hi {y} end" {
		t.Errorf("render = %q", got)
	}

	// Field font applies to literals but not to the variable value.
	f2 := &storage.Field{Template: "ab{x}", Font: "bold"}
	got2 := RenderField(f2, map[string]*storage.Variable{"x": {Name: "x", LastValue: "cd"}})
	if got2 != "𝐚𝐛cd" {
		t.Errorf("render font = %q want 𝐚𝐛cd", got2)
	}
}
