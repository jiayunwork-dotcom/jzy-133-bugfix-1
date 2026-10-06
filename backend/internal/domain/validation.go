package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// 前后端共用的校验逻辑（前端另有镜像实现）。错误消息直接用于 API 响应。

// TargetSpec 建档/更新监控对象所需的字段。
type TargetSpec struct {
	Name         string
	Machine      string
	Dimension    string
	USL          *float64
	LSL          *float64
	SubgroupN    int
	EnabledRules []int
}

// ValidateTarget 校验监控对象配置，返回带具体原因的错误。
func ValidateTarget(s TargetSpec) error {
	var problems []string
	if strings.TrimSpace(s.Name) == "" {
		problems = append(problems, "监控对象名称不能为空")
	}
	if strings.TrimSpace(s.Machine) == "" {
		problems = append(problems, "机床不能为空")
	}
	if strings.TrimSpace(s.Dimension) == "" {
		problems = append(problems, "尺寸名称不能为空")
	}
	if s.USL == nil && s.LSL == nil {
		problems = append(problems, "规格上下限至少填写一侧")
	}
	if s.USL != nil && !finite(*s.USL) {
		problems = append(problems, "规格上限必须是有限数字")
	}
	if s.LSL != nil && !finite(*s.LSL) {
		problems = append(problems, "规格下限必须是有限数字")
	}
	if s.USL != nil && s.LSL != nil && *s.USL <= *s.LSL {
		problems = append(problems, fmt.Sprintf("规格上限(%g)必须大于规格下限(%g)", *s.USL, *s.LSL))
	}
	if !ValidN(s.SubgroupN) {
		problems = append(problems, "子组容量必须是 2 到 10 之间的整数")
	}
	seen := map[int]bool{}
	for _, r := range s.EnabledRules {
		if r < 1 || r > 4 {
			problems = append(problems, fmt.Sprintf("判异规则编号非法: %d", r))
			break
		}
		if seen[r] {
			problems = append(problems, fmt.Sprintf("判异规则重复: 规则%d", r))
			break
		}
		seen[r] = true
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrValidation, strings.Join(problems, "；"))
	}
	return nil
}

// ValidateValues 校验一批录入的测量值：非空且全部为有限数。
func ValidateValues(values []float64) error {
	if len(values) == 0 {
		return fmt.Errorf("%w: 测量值不能为空", ErrBadValues)
	}
	for i, v := range values {
		if !finite(v) {
			return fmt.Errorf("%w: 第 %d 个测量值不是有效数字（不能为 NaN 或无穷）", ErrBadValues, i+1)
		}
	}
	return nil
}

// CheckGroupedSize 校验「整组录入」模式下数量与档案容量一致。
func CheckGroupedSize(count, n int) error {
	if count != n {
		return fmt.Errorf("%w: 本次提交 %d 个值，但档案子组容量为 %d", ErrSizeMismatch, count, n)
	}
	return nil
}

// ValidateBaselineRequest 校验发起基准的参数。
// available 为当前已有子组总数。
func ValidateBaselineRequest(startSeq, endSeq, available int) error {
	var problems []string
	if startSeq < 1 {
		problems = append(problems, "基准期起始子组序号必须 >= 1")
	}
	if endSeq < startSeq {
		problems = append(problems, "基准期结束子组序号不能早于起始序号")
	}
	if endSeq > available {
		problems = append(problems, fmt.Sprintf("基准期结束子组(第%d组)超出已有数据(当前共%d组)", endSeq, available))
	}
	if endSeq-startSeq+1 < 20 {
		problems = append(problems, fmt.Sprintf("基准期至少需要 20 个子组，当前仅 %d 个", endSeq-startSeq+1))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrValidation, strings.Join(problems, "；"))
	}
	return nil
}

// JoinErrors 便于 API 层在已有错误上补充信息。
func JoinErrors(errs ...error) error { return errors.Join(errs...) }

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
