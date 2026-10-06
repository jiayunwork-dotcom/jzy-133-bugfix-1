package api

import (
	"encoding/json"
	"testing"
)

func TestNumberRejectsNonNumbers(t *testing.T) {
	cases := []string{`"abc"`, `true`, `null`, `""`, `[1]`}
	for _, c := range cases {
		var n number
		if err := json.Unmarshal([]byte(c), &n); err == nil {
			t.Errorf("输入 %s 应被拒绝", c)
		}
	}
	for _, c := range []string{`0`, `3.14`, `-2.5e3`} {
		var n number
		if err := json.Unmarshal([]byte(c), &n); err != nil {
			t.Errorf("输入 %s 应合法: %v", c, err)
		}
	}

	// optNumber：缺省/ null -> nil；提供值 -> ptr。
	var o optNumber
	if err := json.Unmarshal([]byte(`null`), &o); err != nil || o.ptr() != nil {
		t.Fatal("null 应解析为缺省")
	}
	if err := json.Unmarshal([]byte(`1.5`), &o); err != nil {
		t.Fatal(err)
	}
	p := o.ptr()
	if p == nil || *p != 1.5 {
		t.Fatalf("应解析为 1.5，实际 %v", p)
	}
}
