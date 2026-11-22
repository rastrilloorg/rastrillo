package op

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/wirename"
)

// readTag reads a field's json tag, and whether json skips the field.
func readTag(
	owner reflect.Type, field reflect.StructField,
) (tag jsonTag, skip bool, err error) {
	text := field.Tag.Get("json")
	if text == "-" || !field.IsExported() && !field.Anonymous {
		return jsonTag{}, true, nil
	}
	name, options, hasOptions := strings.Cut(text, ",")
	tag.name = name
	for option := range strings.SplitSeq(options, ",") {
		switch {
		case !hasOptions:
		case option == "omitzero" || option == "omitempty":
			tag.omit = true
		case option == "inline":
			tag.inline = true
		default:
			return jsonTag{}, false, fmt.Errorf(msgTagOption,
				typeName(owner), field.Name, option)
		}
	}
	return tag, false, nil
}

type jsonTag struct {
	name   string
	omit   bool
	inline bool
}

// property is one member of an object, at the depth of embedding that
// declares it, so dominant can apply json's rule for a repeated name.
type property struct {
	name     string
	depth    int
	schema   schema
	required bool
}

// level is a struct whose fields join an object. optional marks the
// members of an embedded pointer, which json leaves out when it is nil.
type level struct {
	owner    reflect.Type
	depth    int
	optional bool
}

func (walk *walk) object(goType reflect.Type) (schema, error) {
	found, err := walk.collect(level{owner: goType})
	if err != nil {
		return nil, err
	}
	properties, required := schema{}, []string{}
	for _, each := range dominant(found) {
		properties = append(properties, entry{each.name, each.schema})
		if each.required {
			required = append(required, each.name)
		}
	}
	described := schema{{"type", "object"}, {"properties", properties}}
	if len(required) > 0 {
		described = append(described, entry{"required", required})
	}
	return append(described, entry{"additionalProperties", false}), nil
}

func (walk *walk) collect(at level) ([]property, error) {
	var found []property
	for index := range at.owner.NumField() {
		field := at.owner.Field(index)
		tag, skip, err := readTag(at.owner, field)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		if embedded, ok := mergeable(field, tag); ok {
			merged, err := walk.merge(at, field, embedded)
			if err != nil {
				return nil, err
			}
			found = append(found, merged...)
			continue
		}
		added, err := walk.add(at, field, tag)
		if err != nil {
			return nil, err
		}
		found = append(found, added)
	}
	return found, nil
}

// mergeable is an embedded struct with no JSON name. That is the one
// place every json v2 the doors build with flattens members: the pinned
// one and Go 1.27's read inline as an unknown option, and Go 1.26's
// reads it on any field, but all three flatten a Go embedding.
func mergeable(field reflect.StructField, tag jsonTag) (reflect.Type, bool) {
	embedded := field.Type
	if embedded.Kind() == reflect.Pointer {
		embedded = embedded.Elem()
	}
	// json refuses to flatten a type that reads or writes itself.
	ownMethods := slices.Concat(ownJSON[input], ownJSON[output],
		ownText[input], ownText[output])
	return embedded, field.Anonymous && tag.name == "" && !tag.omit &&
		embedded.Kind() == reflect.Struct &&
		!implementsAny(embedded, ownMethods)
}

func (walk *walk) merge(
	at level, field reflect.StructField, embedded reflect.Type,
) ([]property, error) {
	pointer := field.Type.Kind() == reflect.Pointer
	if pointer && walk.way == input {
		return nil, fmt.Errorf(msgPointerInput,
			typeName(at.owner), field.Name)
	}
	if walk.path[embedded] {
		return nil, fmt.Errorf(msgCycle, embedded)
	}
	walk.path[embedded] = true
	defer delete(walk.path, embedded)
	return walk.collect(level{owner: embedded, depth: at.depth + 1,
		optional: at.optional || pointer})
}

func (walk *walk) add(
	at level, field reflect.StructField, tag jsonTag,
) (property, error) {
	where := place{typeName(at.owner), field.Name, field.Type}
	wireName := wirename.Name(field.Name)
	switch {
	case tag.name == "":
		return property{}, fmt.Errorf(msgUntagged,
			where.owner, where.field, wireName)
	case tag.inline:
		// Only an unnamed embedding flattens everywhere; see mergeable.
		return property{}, fmt.Errorf(msgTagOption,
			where.owner, where.field, "inline")
	case tag.name != wireName:
		return property{}, fmt.Errorf(msgWireName,
			where.owner, where.field, tag.name, wireName)
	}
	described, err := walk.describe(field.Type, where)
	required := !at.optional && !tag.omit && !presence(field.Type)
	return property{tag.name, at.depth, described, required}, err
}

// dominant applies json's rule for a name declared more than once: the
// shallowest declaration wins, and two at the same depth cancel out.
func dominant(found []property) []property {
	shallowest := map[string]int{}
	for _, each := range found {
		depth, seen := shallowest[each.name]
		if !seen || each.depth < depth {
			shallowest[each.name] = each.depth
		}
	}
	count := map[string]int{}
	for _, each := range found {
		if each.depth == shallowest[each.name] {
			count[each.name]++
		}
	}
	return slices.DeleteFunc(found, func(each property) bool {
		return each.depth != shallowest[each.name] || count[each.name] > 1
	})
}
