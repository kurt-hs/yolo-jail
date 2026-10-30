package loopholedecl

import (
	"strconv"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/tomlx"
)

// looksLikeTOML says which syntax manifest bytes are in, from the bytes alone: a JSONC manifest
// is an object, so its first token is `{` or a `//` or `/*` comment, and a TOML document can
// begin with neither (a `#` comment or a key). Decode takes bytes and a directory that need not
// exist, so the syntax cannot be read off a file name that may not be there.
func looksLikeTOML(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n', 0xEF, 0xBB, 0xBF:
			continue
		case '{', '/':
			return false
		}
		return true
	}
	return false
}

// decodeTOML reads loophole.toml into the value model the JSONC reader produces, so ONE walk
// validates both forms and the two cannot come to mean different things: tables stay in their
// written order (jail_env, settings and env keep their declaration order) and an integer
// becomes the integer literal JSON5 yields, which the walk's type checks read.
//
// TOML has no null. `yolo pack migrate` refuses a manifest that writes one, naming the key
// (a null has no TOML spelling, RC-D25), rather than guessing what it meant.
func decodeTOML(data []byte) (any, error) {
	root, err := tomlx.DecodeOrdered(data)
	if err != nil {
		return nil, err
	}
	return normalizeTOML(root), nil
}

func normalizeTOML(v any) any {
	switch t := v.(type) {
	case *jsonx.OrderedMap:
		out := jsonx.NewOrderedMap()
		for _, k := range t.Keys() {
			item, _ := t.Get(k)
			out.Set(k, normalizeTOML(item))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = normalizeTOML(item)
		}
		return out
	case int64:
		if lit, ok := jsonx.IntLiteral(strconv.FormatInt(t, 10)); ok {
			return lit
		}
	}
	return v
}
