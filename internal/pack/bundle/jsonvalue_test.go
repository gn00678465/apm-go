package bundle

import (
	"strings"
	"testing"
)

func TestDecodeJSONValue_PreservesObjectKeyOrder(t *testing.T) {
	v, err := DecodeJSONValue([]byte(`{"zebra": 1, "apple": 2, "middle": {"c": 1, "a": 2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.O) != 3 || v.O[0].Key != "zebra" || v.O[1].Key != "apple" || v.O[2].Key != "middle" {
		t.Fatalf("top-level order = %+v, want zebra, apple, middle", v.O)
	}
	nested, _ := v.Get("middle")
	if len(nested.O) != 2 || nested.O[0].Key != "c" || nested.O[1].Key != "a" {
		t.Fatalf("nested order = %+v, want c, a", nested.O)
	}
}

func TestMarshalIndent_RoundTripsInsertionOrder(t *testing.T) {
	v, err := DecodeJSONValue([]byte(`{"zebra":1,"apple":2}`))
	if err != nil {
		t.Fatal(err)
	}
	out := string(MarshalIndent(v))
	zIdx := strings.Index(out, `"zebra"`)
	aIdx := strings.Index(out, `"apple"`)
	if zIdx < 0 || aIdx < 0 || zIdx > aIdx {
		t.Errorf("output = %s, want zebra before apple (insertion order preserved)", out)
	}
	if !strings.Contains(out, "{\n  \"zebra\": 1,\n  \"apple\": 2\n}") {
		t.Errorf("output = %s, want 2-space indent layout", out)
	}
}

func TestMarshalIndent_EmptyObjectAndArray(t *testing.T) {
	obj := ObjectValue(JSONField{Key: "a", Val: JSONValue{Kind: KindObject}}, JSONField{Key: "b", Val: JSONValue{Kind: KindArray}})
	out := string(MarshalIndent(obj))
	if !strings.Contains(out, `"a": {}`) || !strings.Contains(out, `"b": []`) {
		t.Errorf("output = %s, want empty object/array collapsed to {}/[]", out)
	}
}

func TestMarshalIndent_NeverHTMLEscapes(t *testing.T) {
	out := string(MarshalIndent(StringValue("a<b>&c")))
	if out != `"a<b>&c"` {
		t.Errorf("output = %s, want no HTML-escaping of <, >, &", out)
	}
}

func TestSortedClone_SortsObjectKeysRecursively(t *testing.T) {
	v, err := DecodeJSONValue([]byte(`{"zebra":{"z":1,"a":2},"apple":1}`))
	if err != nil {
		t.Fatal(err)
	}
	sorted := v.SortedClone()
	if sorted.O[0].Key != "apple" || sorted.O[1].Key != "zebra" {
		t.Fatalf("top-level sorted order = %+v, want apple, zebra", sorted.O)
	}
	nested, _ := sorted.Get("zebra")
	if nested.O[0].Key != "a" || nested.O[1].Key != "z" {
		t.Fatalf("nested sorted order = %+v, want a, z", nested.O)
	}
}

func TestDeepMerge_NoOverwrite_BaseWins(t *testing.T) {
	base, _ := DecodeJSONValue([]byte(`{"shared":"base","onlyBase":1}`))
	overlay, _ := DecodeJSONValue([]byte(`{"shared":"overlay","onlyOverlay":2}`))
	merged, err := DeepMerge(base, overlay, false)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := merged.Get("shared")
	if shared.S != "base" {
		t.Errorf("shared = %q, want base to win when overwrite=false", shared.S)
	}
	if _, ok := merged.Get("onlyBase"); !ok {
		t.Error("onlyBase missing from merge result")
	}
	if _, ok := merged.Get("onlyOverlay"); !ok {
		t.Error("onlyOverlay missing from merge result")
	}
}

func TestDeepMerge_Overwrite_OverlayWins(t *testing.T) {
	base, _ := DecodeJSONValue([]byte(`{"shared":"base"}`))
	overlay, _ := DecodeJSONValue([]byte(`{"shared":"overlay"}`))
	merged, err := DeepMerge(base, overlay, true)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := merged.Get("shared")
	if shared.S != "overlay" {
		t.Errorf("shared = %q, want overlay to win when overwrite=true", shared.S)
	}
}

func TestDeepMerge_RecursesNestedObjects(t *testing.T) {
	base, _ := DecodeJSONValue([]byte(`{"nested":{"a":1}}`))
	overlay, _ := DecodeJSONValue([]byte(`{"nested":{"b":2}}`))
	merged, err := DeepMerge(base, overlay, false)
	if err != nil {
		t.Fatal(err)
	}
	nested, _ := merged.Get("nested")
	if _, ok := nested.Get("a"); !ok {
		t.Error("nested.a missing after merge")
	}
	if _, ok := nested.Get("b"); !ok {
		t.Error("nested.b missing after merge")
	}
}

func TestDeepMerge_ExceedsMaxDepth_Errors(t *testing.T) {
	// Build a chain of nested objects 22 levels deep in both base and
	// overlay so recursion must exceed maxMergeDepth (20).
	build := func(leafKey string) JSONValue {
		v := ObjectValue(JSONField{Key: leafKey, Val: StringValue("leaf")})
		for i := 0; i < 25; i++ {
			v = ObjectValue(JSONField{Key: "n", Val: v})
		}
		return v
	}
	base := build("a")
	overlay := build("b")
	if _, err := DeepMerge(base, overlay, false); err == nil {
		t.Fatal("expected an error for merge nesting beyond maxMergeDepth")
	}
}

// Finding 6 (F09): every upstream writer behind this marshaller
// (_helpers.py:662, plugin_exporter.py:1004-1017,
// agent_plugin_exporter.py:473-478) is json.dumps with the default
// ensure_ascii=True, so non-ASCII must become \uXXXX -- with a UTF-16
// surrogate pair for non-BMP runes, exactly as CPython emits them.
func TestMarshalIndent_EnsureASCII(t *testing.T) {
	got := string(MarshalIndent(ObjectValue(
		JSONField{Key: "name", Val: StringValue("名<")},
		JSONField{Key: "emoji", Val: StringValue("a😀b")},
		JSONField{Key: "ctl", Val: StringValue("x\x01y\x7f")},
		// Round-2 F6: Python's ESCAPE_DCT uses the short forms for \b and \f
		// (json/encoder.py), not \u0008/\u000c.
		JSONField{Key: "short", Val: StringValue("\b\f\n\r\t")},
	)))
	want := "{\n" +
		"  \"name\": \"\\u540d<\",\n" +
		"  \"emoji\": \"a\\ud83d\\ude00b\",\n" +
		"  \"ctl\": \"x\\u0001y\\u007f\",\n" +
		"  \"short\": \"\\b\\f\\n\\r\\t\"\n" +
		"}"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
