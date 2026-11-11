package commandtemplate

import (
	"reflect"
	"testing"
)

func TestV1CommandTemplateVectors(t *testing.T) {
	// Source: own v1 renderer/utils/commandTemplate.ts:3,9-20 and its TS test echo vector.
	tests := []struct {
		name, in, out string
		p             map[string]string
		keys          []string
	}{
		{"ts-echo", "echo [#value]", "echo x", map[string]string{"value": "x"}, []string{"value"}},
		{"dedup-order", "[#b] [#a] [#b]", "B A B", map[string]string{"a": "A", "b": "B"}, []string{"b", "a"}},
		{"missing", "[#a]-[#unset]", "A-", map[string]string{"a": "A"}, []string{"a", "unset"}},
		{"literal", "[#a]", "$1\\n[#b]", map[string]string{"a": "$1\\n[#b]", "b": "not-recursive"}, []string{"a"}},
		{"ascii-word", "[#中文] [#a-b] [#_2]", "[#中文] [#a-b] ok", map[string]string{"_2": "ok"}, []string{"_2"}},
		{"shell", "echo ${HOME}", "echo ${HOME}", nil, []string{}},
	}
	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			if got := Render(v.in, v.p); got != v.out {
				t.Fatal(got)
			}
			if got := Keys(v.in); !reflect.DeepEqual(got, v.keys) {
				t.Fatal(got, v.keys)
			}
		})
	}
}
