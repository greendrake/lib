package ipcam

import (
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Key names a setting, "<group>.<name>": "network.address".
type Key string

// group is the ID of the group the key's setting is in.
func (k Key) group() string {
	group, _, _ := strings.Cut(string(k), ".")
	return group
}

// StreamSetting is the key of one of a stream's settings: "encode.main.resolution".
func StreamSetting(stream StreamKey, name string) Key {
	return Key("encode." + string(stream) + "." + name)
}

// Placement is the keys of where on the picture an overlay is drawn, by its top left:
// "overlay.nameLeft" and "overlay.nameTop".
func Placement(overlay string) (left, top Key) {
	return Key("overlay." + overlay + "Left"), Key("overlay." + overlay + "Top")
}

// DSTPoint is when daylight saving starts, or ends.
type DSTPoint string

const (
	DSTStart DSTPoint = "dstStart"
	DSTEnd   DSTPoint = "dstEnd"
)

// DSTSetting is the key of one of a daylight saving point's settings:
// "general.dstStart.month".
func DSTSetting(point DSTPoint, name string) Key {
	return Key("general." + string(point) + "." + name)
}

// KindType is what a setting's value is, and how it is edited.
type KindType string

// The kinds, and the form their values take.
const (
	// A bool.
	Switch KindType = "switch"
	// A whole number, an int, within Min and Max where it has them.
	Integer KindType = "integer"
	// A number from 0 to 1, a float64: a share of the picture, across or down from its
	// top left.
	Fraction KindType = "fraction"
	// A string, of MinLength to MaxLength characters where it has them, none of them
	// Excluded.
	Text KindType = "text"
	// A dotted quad, as a string.
	IPv4 KindType = "ipv4"
	// One of Options' values, a string.
	Choice KindType = "choice"
	// A string, shown and never written.
	ReadOnly KindType = "readonly"
	// A string, shown, and written only as the kind's Only.
	Pinned KindType = "pinned"
)

// Kind is what a setting's value is, and what bounds it.
type Kind struct {
	Type KindType
	Min  *int `json:",omitempty"`
	Max  *int `json:",omitempty"`
	// A text's bounds, in characters, and the characters it cannot have.
	MinLength *int   `json:",omitempty"`
	MaxLength *int   `json:",omitempty"`
	Excluded  string `json:",omitempty"`
	// A choice's options.
	Options []Option `json:",omitempty"`
	// What a pinned setting is written as.
	Only string `json:",omitempty"`
}

// Option is one of a choice's. Its value names what it means, never how a camera encodes
// it.
type Option struct {
	Value string
	Label string
}

// Group is a page of settings.
type Group struct {
	ID    string
	Title string
}

// Spec is the description of a setting, in the group its key names.
type Spec struct {
	Key   Key
	Label string
	// What the label leaves unsaid; empty where it says it all.
	Tip  string
	Kind Kind
}

// Setting is one of a camera's settings: the catalogue's description of it, with the
// camera's Specifics laid over it, and its value. Its kind's fields are its own on the
// wire, so that a client tells its kinds apart by Type.
type Setting struct {
	Key   Key
	Group string
	Label string
	Tip   string
	Kind
	Value any
}

// Specifics are what a camera adds to the catalogue's description of a setting.
type Specifics struct {
	// A choice's options, where the camera decides them.
	Options []Option
	// What the camera does that its kind does not say, appended to the tip.
	Note string
}

// UnknownSetting is a key the catalogue or the camera has no setting of.
type UnknownSetting struct {
	Key Key
}

func (e *UnknownSetting) Error() string {
	return fmt.Sprintf("ipcam: no setting %q", e.Key)
}

// InvalidValue is a value a setting does not take, with why.
type InvalidValue struct {
	Problem string
}

func (e *InvalidValue) Error() string {
	return "ipcam: " + e.Problem
}

// UnknownGroup is a group the catalogue has none of.
type UnknownGroup struct {
	ID string
}

func (e *UnknownGroup) Error() string {
	return fmt.Sprintf("ipcam: no group %q", e.ID)
}

// Extension is an adapter's settings beyond the core's catalogue, which every camera of
// the adapter's protocol can have: each in the group its key names, one of the core's or
// one of Groups, which show after the core's.
type Extension struct {
	Groups   []Group
	Settings []Spec
}

// pages are the core's catalogue's with e's settings: those of a group of the core's
// after the core's, those of e's own groups on pages of their own. A setting in no group,
// or of a key the catalogue has already, is a mistake in the adapter, and panics.
func (e Extension) pages() []page {
	pages := slices.Clone(core)
	for _, group := range e.Groups {
		pages = append(pages, page{group: group})
	}
	keys := map[Key]bool{}
	for _, p := range core {
		for _, s := range p.settings {
			keys[s.Key] = true
		}
	}
	for _, s := range e.Settings {
		i := slices.IndexFunc(pages, func(p page) bool { return p.group.ID == s.Key.group() })
		if i < 0 || keys[s.Key] {
			panic(fmt.Sprintf("ipcam: an extension's setting %s is in no group, or the catalogue's already", s.Key))
		}
		keys[s.Key] = true
		// Clipped, so that the core's pages are never appended to in place.
		pages[i].settings = append(slices.Clip(pages[i].settings), s)
	}
	return pages
}

// Keys are every setting's key a camera of the extension's adapter can have, in the
// catalogue's order.
func (e Extension) Keys() []Key {
	var keys []Key
	for _, p := range e.pages() {
		for _, s := range p.settings {
			keys = append(keys, s.Key)
		}
	}
	return keys
}

// Catalogue is the settings one login has, described: those of the core's catalogue and
// of its adapter's Extension the camera has, with what it adds to their descriptions.
type Catalogue struct {
	pages     []page
	specifics map[Key]Specifics
	// The settings the camera has, by key, with their groups' IDs.
	has map[Key]entry
}

// entry is a setting of a catalogue, with its group's ID.
type entry struct {
	Spec
	group string
}

// NewCatalogue is the catalogue of a login of ext's adapter whose camera has the settings
// specifics names: what it adds to the description of each.
func NewCatalogue(ext Extension, specifics map[Key]Specifics) *Catalogue {
	c := &Catalogue{pages: ext.pages(), specifics: specifics, has: map[Key]entry{}}
	for _, p := range c.pages {
		for _, s := range p.settings {
			if _, has := specifics[s.Key]; has {
				c.has[s.Key] = entry{s, p.group.ID}
			}
		}
	}
	return c
}

// Groups are the groups of settings the camera has, in order: those it has a setting of,
// and those shown whatever it has.
func (c *Catalogue) Groups() []Group {
	groups := []Group{}
	for _, p := range c.pages {
		if p.always || slices.ContainsFunc(p.settings, func(s Spec) bool { _, has := c.has[s.Key]; return has }) {
			groups = append(groups, p.group)
		}
	}
	return groups
}

// Keys are the keys of the settings of group the camera has, in order.
func (c *Catalogue) Keys(group string) ([]Key, error) {
	i := slices.IndexFunc(c.pages, func(p page) bool { return p.group.ID == group })
	if i < 0 {
		return nil, &UnknownGroup{ID: group}
	}
	keys := []Key{}
	for _, s := range c.pages[i].settings {
		if _, has := c.has[s.Key]; has {
			keys = append(keys, s.Key)
		}
	}
	return keys, nil
}

// described is e as the camera has it, its value left out.
func (c *Catalogue) described(e entry) Setting {
	s := c.specifics[e.Key]
	kind := e.Kind
	if s.Options != nil {
		kind.Options = s.Options
	}
	tip := strings.Join(slices.DeleteFunc([]string{e.Tip, s.Note}, func(part string) bool { return part == "" }), " ")
	return Setting{Key: e.Key, Group: e.group, Label: e.Label, Tip: tip, Kind: kind}
}

// Lookup describes the camera's setting of key, its value left out.
func (c *Catalogue) Lookup(key Key) (Setting, error) {
	e, has := c.has[key]
	if !has {
		return Setting{}, &UnknownSetting{Key: key}
	}
	return c.described(e), nil
}

// Describe describes the settings values has that the camera has, in the catalogue's
// order.
func (c *Catalogue) Describe(values map[Key]any) []Setting {
	settings := []Setting{}
	for _, p := range c.pages {
		for _, s := range p.settings {
			value, valued := values[s.Key]
			e, has := c.has[s.Key]
			if !valued || !has {
				continue
			}
			setting := c.described(e)
			setting.Value = value
			settings = append(settings, setting)
		}
	}
	return settings
}

// Check makes value what a setting of kind takes, in the kind's form, or answers
// InvalidValue. value is as encoding/json decodes one into an any, a number a float64, or
// already in the kind's form.
func Check(kind Kind, value any) (any, error) {
	check, writable := checks[kind.Type]
	if !writable {
		return nil, &InvalidValue{Problem: "the setting is read only"}
	}
	return check(kind, value)
}

// checks make a value of each kind written what the kind takes.
var checks = map[KindType]func(kind Kind, value any) (any, error){
	Switch: func(_ Kind, value any) (any, error) {
		return is[bool](value, "true or false")
	},
	Integer: func(kind Kind, value any) (any, error) {
		what := wholeNumber(kind)
		var n int
		switch v := value.(type) {
		case int:
			n = v
		case float64:
			// Past 2^53, a float64 holds whole numbers alone, and not every one of them.
			if v != math.Trunc(v) || math.Abs(v) > 1<<53 {
				return nil, not(what)
			}
			n = int(v)
		default:
			return nil, not(what)
		}
		if kind.Min != nil && n < *kind.Min || kind.Max != nil && n > *kind.Max {
			return nil, not(what)
		}
		return n, nil
	},
	Fraction: func(_ Kind, value any) (any, error) {
		const share = "a share of the picture, from 0 to 1"
		f, err := is[float64](value, share)
		if err == nil && (f < 0 || f > 1) {
			return nil, not(share)
		}
		return f, err
	},
	Text: func(kind Kind, value any) (any, error) {
		text := aText(kind)
		s, err := is[string](value, text)
		n := utf8.RuneCountInString(s)
		if err == nil && (kind.MinLength != nil && n < *kind.MinLength || kind.MaxLength != nil && n > *kind.MaxLength || strings.ContainsAny(s, kind.Excluded)) {
			return nil, not(text)
		}
		return s, err
	},
	IPv4: func(_ Kind, value any) (any, error) {
		const address = "an IPv4 address: four numbers to 255 joined by dots"
		s, err := is[string](value, address)
		if err != nil {
			return nil, err
		}
		if parsed, err := netip.ParseAddr(s); err != nil || !parsed.Is4() {
			return nil, not(address)
		}
		return s, nil
	},
	Choice: func(kind Kind, value any) (any, error) {
		s, err := is[string](value, "an option's value")
		if err == nil && !slices.ContainsFunc(kind.Options, func(o Option) bool { return o.Value == s }) {
			return nil, &InvalidValue{Problem: fmt.Sprintf("%q is none of the options", s)}
		}
		return s, err
	},
	Pinned: func(kind Kind, value any) (any, error) {
		s, err := is[string](value, kind.Only)
		if err == nil && s != kind.Only {
			return nil, &InvalidValue{Problem: "it is written as " + kind.Only + " alone"}
		}
		return s, err
	},
}

// is is value as a T, or answers that it is not what, which a T is.
func is[T any](value any, what string) (T, error) {
	v, ok := value.(T)
	if !ok {
		return v, not(what)
	}
	return v, nil
}

// not answers that a value is not what the setting takes.
func not(what string) *InvalidValue {
	return &InvalidValue{Problem: "not " + what}
}

// wholeNumber says what an integer of kind takes.
func wholeNumber(kind Kind) string {
	bounds := []string{"a whole number"}
	if kind.Min != nil {
		bounds = append(bounds, fmt.Sprintf("from %d", *kind.Min))
	}
	if kind.Max != nil {
		bounds = append(bounds, fmt.Sprintf("to %d", *kind.Max))
	}
	return strings.Join(bounds, " ")
}

// aText says what a text of kind takes.
func aText(kind Kind) string {
	text := "a text"
	switch least, most := kind.MinLength, kind.MaxLength; {
	case least != nil && most != nil:
		text += fmt.Sprintf(" of %d to %d characters", *least, *most)
	case least != nil:
		text += fmt.Sprintf(" of at least %d characters", *least)
	case most != nil:
		text += fmt.Sprintf(" of at most %d characters", *most)
	}
	if kind.Excluded != "" {
		text += " without " + strings.Join(strings.Split(kind.Excluded, ""), " or ")
	}
	return text
}

// zoneLayout is the form of a time zone's option value, its offset from UTC: "+12:00".
const zoneLayout = "-07:00"

// ZoneOption is the value of the time zone option minutesEast of UTC.
func ZoneOption(minutesEast int) string {
	return time.Unix(0, 0).In(time.FixedZone("", minutesEast*60)).Format(zoneLayout)
}

// ZoneMinutes is how many minutes east of UTC the time zone option is.
func ZoneMinutes(option string) (int, error) {
	t, err := time.Parse(zoneLayout, option)
	if err != nil {
		return 0, err
	}
	_, offset := t.Zone()
	return offset / 60, nil
}
