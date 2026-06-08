package variables

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// ConfigKind classifies what extra configuration a predefined type needs, so
// the bot UI can run the right creation sub-flow.
type ConfigKind string

const (
	KindNone    ConfigKind = "none"
	KindText    ConfigKind = "text"    // custom: a literal (possibly multi-line) string
	KindTZ      ConfigKind = "tz"      // needs a timezone
	KindTime    ConfigKind = "time"    // needs a timezone + a time layout
	KindDate    ConfigKind = "date"    // needs a calendar + a timezone
	KindList    ConfigKind = "list"    // a set of values (cycled or random)
	KindCounter ConfigKind = "counter" // start + step
)

// Spec describes a predefined variable type.
type Spec struct {
	Key             string
	FA              string
	EN              string
	Kind            ConfigKind
	DefaultInterval int
	DefaultConfig   map[string]any
	// compute returns the raw (pre-font) value and the next cursor.
	compute func(cfg map[string]any, cursor int, now time.Time) (string, int)
}

// CustomType is the sentinel type for user-defined literal text variables.
const CustomType = "custom"

var processStart = time.Now()

// Specs returns all predefined variable specs in display order.
func Specs() []Spec { return specList }

// SpecByKey returns the spec for a predefined key (ok=false if unknown).
func SpecByKey(key string) (Spec, bool) {
	s, ok := specIndex[key]
	return s, ok
}

// ---------------------------------------------------------------------------
// Compute helpers
// ---------------------------------------------------------------------------

func loadLoc(cfg map[string]any) *time.Location {
	tz, _ := cfg["tz"].(string)
	if tz == "" {
		tz = "UTC"
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

func cfgList(cfg map[string]any, key string) []string {
	raw, ok := cfg[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			out = append(out, fmt.Sprint(e))
		}
		return out
	}
	return nil
}

func cfgInt(cfg map[string]any, key string, def int) int {
	switch v := cfg[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func cycle(items []string, cursor int) (string, int) {
	if len(items) == 0 {
		return "", 0
	}
	idx := ((cursor % len(items)) + len(items)) % len(items)
	return items[idx], cursor + 1
}

// ---------------------------------------------------------------------------
// Jalali (Persian) calendar conversion
// ---------------------------------------------------------------------------

func gregorianToJalali(gy, gm, gd int) (int, int, int) {
	gDaysInMonth := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	jDaysInMonth := []int{31, 31, 31, 31, 31, 31, 30, 30, 30, 30, 30, 29}

	gy2 := gy - 1600
	gm2 := gm - 1
	gd2 := gd - 1

	gDayNo := 365*gy2 + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400
	for i := 0; i < gm2; i++ {
		gDayNo += gDaysInMonth[i]
	}
	if gm2 > 1 && ((gy%4 == 0 && gy%100 != 0) || gy%400 == 0) {
		gDayNo++
	}
	gDayNo += gd2

	jDayNo := gDayNo - 79
	jNp := jDayNo / 12053
	jDayNo %= 12053
	jy := 979 + 33*jNp + 4*(jDayNo/1461)
	jDayNo %= 1461
	if jDayNo >= 366 {
		jy += (jDayNo - 1) / 365
		jDayNo = (jDayNo - 1) % 365
	}
	var jm, jd int
	for i := 0; i < 11 && jDayNo >= jDaysInMonth[i]; i++ {
		jDayNo -= jDaysInMonth[i]
		jm = i + 1
	}
	jm++
	jd = jDayNo + 1
	return jy, jm, jd
}

var persianMonths = []string{
	"فروردین", "اردیبهشت", "خرداد", "تیر", "مرداد", "شهریور",
	"مهر", "آبان", "آذر", "دی", "بهمن", "اسفند",
}

// persianWeekday maps a Go weekday to its Persian name.
var persianWeekday = map[time.Weekday]string{
	time.Saturday:  "شنبه",
	time.Sunday:    "یکشنبه",
	time.Monday:    "دوشنبه",
	time.Tuesday:   "سه‌شنبه",
	time.Wednesday: "چهارشنبه",
	time.Thursday:  "پنجشنبه",
	time.Friday:    "جمعه",
}

// ---------------------------------------------------------------------------
// Default emoji / value sets
// ---------------------------------------------------------------------------

var (
	flagSet    = []string{"🇮🇷", "🇺🇸", "🇬🇧", "🇩🇪", "🇫🇷", "🇮🇹", "🇪🇸", "🇯🇵", "🇰🇷", "🇨🇳", "🇹🇷", "🇦🇪", "🇨🇦", "🇧🇷", "🇷🇺"}
	laughSet   = []string{"😂", "🤣", "😅", "😆", "😄", "😁", "😹"}
	heartSet   = []string{"❤️", "🧡", "💛", "💚", "💙", "💜", "🤎", "🖤", "🤍", "💖"}
	moonSet    = []string{"🌑", "🌒", "🌓", "🌔", "🌕", "🌖", "🌗", "🌘"}
	starSet    = []string{"✨", "⭐️", "🌟", "💫", "⚡️", "🌠"}
	animalSet  = []string{"🐶", "🐱", "🦊", "🐻", "🐼", "🐨", "🦁", "🐯", "🐸", "🐵"}
	plantSet   = []string{"🌸", "🌺", "🌻", "🌷", "🌹", "🌼", "🌿", "🍀", "🌱", "🪷"}
	weatherSet = []string{"☀️", "⛅️", "🌤️", "🌧️", "⛈️", "🌩️", "❄️", "🌈", "🌪️"}
	clockFaces = []string{"🕛", "🕐", "🕑", "🕒", "🕓", "🕔", "🕕", "🕖", "🕗", "🕘", "🕙", "🕚"}
	quoteSet   = []string{"به جلو", "آرام باش", "هرگز تسلیم نشو", "امروز روز توست", "لبخند بزن"}
	emojiPool  = []string{"🔥", "💎", "🚀", "🌟", "🎯", "💯", "⚡️", "🎨", "🌈", "🍃", "🦋", "🌙", "☄️", "🪐"}
)

// listType builds a Spec for a value-set variable (cycled).
func listType(key, fa, en string, def []string, interval int) Spec {
	return Spec{
		Key: key, FA: fa, EN: en, Kind: KindList, DefaultInterval: interval,
		DefaultConfig: map[string]any{"items": def},
		compute: func(cfg map[string]any, cursor int, _ time.Time) (string, int) {
			items := cfgList(cfg, "items")
			if len(items) == 0 {
				items = def
			}
			return cycle(items, cursor)
		},
	}
}

var specList = []Spec{
	{
		Key: "time", FA: "⏰ زمان فعلی", EN: "⏰ Current time", Kind: KindTime, DefaultInterval: 60,
		DefaultConfig: map[string]any{"tz": "Asia/Tehran", "layout": "15:04"},
		compute: func(cfg map[string]any, cursor int, now time.Time) (string, int) {
			layout, _ := cfg["layout"].(string)
			if layout == "" {
				layout = "15:04"
			}
			return now.In(loadLoc(cfg)).Format(layout), cursor
		},
	},
	{
		Key: "date", FA: "📅 تاریخ", EN: "📅 Date", Kind: KindDate, DefaultInterval: 3600,
		DefaultConfig: map[string]any{"tz": "Asia/Tehran", "cal": "jalali"},
		compute: func(cfg map[string]any, cursor int, now time.Time) (string, int) {
			t := now.In(loadLoc(cfg))
			if cal, _ := cfg["cal"].(string); cal == "jalali" {
				jy, jm, jd := gregorianToJalali(t.Year(), int(t.Month()), t.Day())
				return fmt.Sprintf("%d %s %d", jd, persianMonths[jm-1], jy), cursor
			}
			return t.Format("2006-01-02"), cursor
		},
	},
	{
		Key: "weekday", FA: "📆 روز هفته", EN: "📆 Weekday", Kind: KindDate, DefaultInterval: 3600,
		DefaultConfig: map[string]any{"tz": "Asia/Tehran", "cal": "jalali"},
		compute: func(cfg map[string]any, cursor int, now time.Time) (string, int) {
			t := now.In(loadLoc(cfg))
			if cal, _ := cfg["cal"].(string); cal == "jalali" {
				return persianWeekday[t.Weekday()], cursor
			}
			return t.Weekday().String(), cursor
		},
	},
	{
		Key: "clock_face", FA: "🕐 ساعت ایموجی", EN: "🕐 Clock emoji", Kind: KindTZ, DefaultInterval: 1800,
		DefaultConfig: map[string]any{"tz": "Asia/Tehran"},
		compute: func(cfg map[string]any, cursor int, now time.Time) (string, int) {
			h := now.In(loadLoc(cfg)).Hour() % 12
			return clockFaces[h], cursor
		},
	},
	listType("flags", "🚩 پرچم‌ها", "🚩 Flags", flagSet, 30),
	listType("laugh", "😂 خنده", "😂 Laughs", laughSet, 20),
	listType("hearts", "❤️ قلب‌ها", "❤️ Hearts", heartSet, 30),
	listType("moon", "🌙 فازهای ماه", "🌙 Moon phases", moonSet, 60),
	listType("stars", "✨ ستاره‌ها", "✨ Stars", starSet, 30),
	listType("animals", "🐶 حیوانات", "🐶 Animals", animalSet, 60),
	listType("plants", "🌸 گل و گیاه", "🌸 Plants", plantSet, 60),
	listType("weather", "🌦 آب‌وهوا (نمادین)", "🌦 Weather (decor)", weatherSet, 120),
	{
		Key: "random_emoji", FA: "🎲 ایموجی تصادفی", EN: "🎲 Random emoji", Kind: KindList, DefaultInterval: 45,
		DefaultConfig: map[string]any{"items": emojiPool},
		compute: func(cfg map[string]any, cursor int, _ time.Time) (string, int) {
			items := cfgList(cfg, "items")
			if len(items) == 0 {
				items = emojiPool
			}
			return items[rand.Intn(len(items))], cursor + 1
		},
	},
	listType("quote", "💬 نقل‌قول", "💬 Quote", quoteSet, 300),
	{
		Key: "counter", FA: "🔢 شمارنده", EN: "🔢 Counter", Kind: KindCounter, DefaultInterval: 60,
		DefaultConfig: map[string]any{"start": 0, "step": 1},
		compute: func(cfg map[string]any, cursor int, _ time.Time) (string, int) {
			start := cfgInt(cfg, "start", 0)
			step := cfgInt(cfg, "step", 1)
			return strconv.Itoa(start + cursor*step), cursor + 1
		},
	},
	{
		Key: "uptime", FA: "⏳ مدت روشن‌بودن", EN: "⏳ Uptime", Kind: KindNone, DefaultInterval: 60,
		DefaultConfig: map[string]any{},
		compute: func(_ map[string]any, cursor int, now time.Time) (string, int) {
			d := now.Sub(processStart).Truncate(time.Minute)
			h := int(d.Hours())
			mnt := int(d.Minutes()) % 60
			if h > 0 {
				return fmt.Sprintf("%dh%02dm", h, mnt), cursor
			}
			return fmt.Sprintf("%dm", mnt), cursor
		},
	},
}

var specIndex = func() map[string]Spec {
	m := make(map[string]Spec, len(specList))
	for _, s := range specList {
		m[s.Key] = s
	}
	return m
}()

// Timezones offered in the UI (label -> IANA name).
var Timezones = []struct{ Label, TZ string }{
	{"Tehran 🇮🇷", "Asia/Tehran"},
	{"Dubai 🇦🇪", "Asia/Dubai"},
	{"Istanbul 🇹🇷", "Europe/Istanbul"},
	{"London 🇬🇧", "Europe/London"},
	{"Berlin 🇩🇪", "Europe/Berlin"},
	{"Moscow 🇷🇺", "Europe/Moscow"},
	{"New York 🇺🇸", "America/New_York"},
	{"Los Angeles 🇺🇸", "America/Los_Angeles"},
	{"Tokyo 🇯🇵", "Asia/Tokyo"},
	{"UTC 🌍", "UTC"},
}

// TimeLayouts offered in the UI (label -> Go layout).
var TimeLayouts = []struct{ Label, Layout string }{
	{"24h  15:04", "15:04"},
	{"24h+s  15:04:05", "15:04:05"},
	{"12h  03:04 PM", "03:04 PM"},
	{"15:04 MST", "15:04 MST"},
	{"Mon 15:04", "Mon 15:04"},
	{"02 Jan 15:04", "02 Jan 15:04"},
}
