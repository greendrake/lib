package rpcserver

import "reflect"

// mergePatch is the RFC 7386 merge patch that turns the JSON object prev into next: a
// member next lacks is null, one whose value changed carries the new value — patched in
// turn where both values are objects — and an unchanged one is left out. Both are
// decoded JSON (map[string]any, []any, string, float64, bool), and neither may hold a
// null member, which a merge patch cannot tell from a removal. changed is false when
// the two are equal, the patch then being empty.
func mergePatch(prev, next map[string]any) (patch map[string]any, changed bool) {
	patch = map[string]any{}
	for key := range prev {
		if _, kept := next[key]; !kept {
			patch[key] = nil
		}
	}
	for key, value := range next {
		was, had := prev[key]
		if !had {
			patch[key] = value
			continue
		}
		wasObject, wasIsObject := was.(map[string]any)
		object, isObject := value.(map[string]any)
		if wasIsObject && isObject {
			if sub, subChanged := mergePatch(wasObject, object); subChanged {
				patch[key] = sub
			}
			continue
		}
		if !reflect.DeepEqual(was, value) {
			patch[key] = value
		}
	}
	return patch, len(patch) > 0
}
