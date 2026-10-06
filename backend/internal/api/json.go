package api

import (
	"encoding/json"
	"fmt"
	"math"
)

// number 只接受有限 JSON 数字：拒绝 NaN/Infinity（标准 JSON 本不支持，
// 但 encoding/json 对字符串形式可能兼容）以及字符串/布尔/null。
type number float64

func (n *number) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	f, ok := v.(float64)
	if !ok {
		return fmt.Errorf("必须是数字")
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("必须是有限数字")
	}
	*n = number(f)
	return nil
}

// optNumber 可缺省的有限数字，零值表示「未提供」。
type optNumber struct {
	set   bool
	value float64
}

func (o *optNumber) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var n number
	if err := n.UnmarshalJSON(b); err != nil {
		return err
	}
	o.set = true
	o.value = float64(n)
	return nil
}

func (o optNumber) ptr() *float64 {
	if !o.set {
		return nil
	}
	v := o.value
	return &v
}
