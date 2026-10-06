package domain

// MakeSubgroup 由顺序号与一组测量值生成子组统计（均值、极差）。
func MakeSubgroup(seq int, values []float64) Subgroup {
	min := values[0]
	max := values[0]
	sum := 0.0
	for _, v := range values {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	cp := make([]float64, len(values))
	copy(cp, values)
	return Subgroup{
		Seq:    seq,
		Mean:   sum / float64(len(values)),
		Range:  max - min,
		Values: cp,
	}
}
