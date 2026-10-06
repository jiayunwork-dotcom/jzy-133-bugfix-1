package store

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
)

// DB 包装数据库连接，全部 SQL 集中在 store 包。
type DB struct{ *sql.DB }

func New(db *sql.DB) *DB { return &DB{db} }

// TargetRow targets 表一行。
type TargetRow struct {
	ID           int64
	Name         string
	Machine      string
	Dimension    string
	USL          sql.NullFloat64
	LSL          sql.NullFloat64
	SubgroupN    int
	EnabledRules pq.Int64Array
	CreatedAt    time.Time
}

// SubgroupRow subgroups 表一行。
type SubgroupRow struct {
	Seq       int
	Mean      float64
	Range     float64
	CreatedAt time.Time
}

// BaselineRow baselines 表一行。
type BaselineRow struct {
	ID            int64
	Version       int
	RefStartSeq   int
	RefEndSeq     int
	EffectiveFrom int
	EffectiveTo   sql.NullInt64
	XBarBar       float64
	RBar          float64
	UCLX          float64
	LCLX          float64
	UCLR          float64
	LCLR          float64
	SigmaWithin   float64
	Cp            sql.NullFloat64
	Cpk           sql.NullFloat64
	CreatedAt     time.Time
}

// AlarmRow alarms 表一行。
type AlarmRow struct {
	Rule        int
	TriggerSeq  int
	InvolvedSeq pq.Int64Array
	CreatedAt   time.Time
}

// MeasurementRow measurements 表一行。
type MeasurementRow struct {
	Seq         int64
	Value       float64
	SubgroupSeq sql.NullInt64
}

// IntsToInt64 把 []int 转成驱动用的 pq.Int64Array。
func IntsToInt64(in []int) pq.Int64Array {
	out := make(pq.Int64Array, len(in))
	for i, v := range in {
		out[i] = int64(v)
	}
	return out
}
