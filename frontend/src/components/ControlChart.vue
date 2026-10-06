<template>
  <div class="panel">
    <h2>均值–极差控制图（X̄–R）</h2>
    <svg
      v-if="geom"
      :viewBox="`0 0 ${geom.width} ${geom.height}`"
      width="100%"
      role="img"
      aria-label="均值极差控制图"
    >
      <!-- Xbar 图 -->
      <g class="chart-x">
        <rect :x="m.l" :y="m.t" :width="plotW" :height="chartH" fill="#fafbfc" />
        <!-- 规格线（参考） -->
        <g v-if="specLines.x.length">
          <line
            v-for="(l, i) in specLines.x" :key="'spec'+i"
            :x1="m.l" :x2="m.l + plotW"
            :y1="y(l.value, scales.x)" :y2="y(l.value, scales.x)"
            stroke="#1a7f4b" stroke-width="1" stroke-dasharray="2 4"
          />
          <text
            v-for="(l, i) in specLines.x" :key="'spect'+i"
            :x="m.l + 4" :y="y(l.value, scales.x) - 3"
            fill="#1a7f4b" font-size="11"
          >{{ l.label }}</text>
        </g>

        <!-- 控制限分段 -->
        <g v-for="(seg, i) in segments.x" :key="'segx'+i">
          <line
            :x1="cx(seg.from)" :x2="cx(seg.to)"
            :y1="y(seg.ucl, scales.x)" :y2="y(seg.ucl, scales.x)"
            stroke="#b25e00" stroke-width="1.4" stroke-dasharray="6 3"
          />
          <line
            :x1="cx(seg.from)" :x2="cx(seg.to)"
            :y1="y(seg.lcl, scales.x)" :y2="y(seg.lcl, scales.x)"
            stroke="#b25e00" stroke-width="1.4" stroke-dasharray="6 3"
          />
          <line
            :x1="cx(seg.from)" :x2="cx(seg.to)"
            :y1="y(seg.cl, scales.x)" :y2="y(seg.cl, scales.x)"
            stroke="#333" stroke-width="1.2"
          />
          <text :x="cx(seg.to) - 4" :y="y(seg.ucl, scales.x) - 3"
                text-anchor="end" font-size="10" fill="#b25e00">v{{ seg.version }} UCL</text>
          <text :x="cx(seg.to) - 4" :y="y(seg.cl, scales.x) - 3"
                text-anchor="end" font-size="10" fill="#333">CL</text>
          <text :x="cx(seg.to) - 4" :y="y(seg.lcl, scales.x) + 10"
                text-anchor="end" font-size="10" fill="#b25e00">LCL</text>
        </g>

        <!-- 重新基准分界 -->
        <g v-for="(b, i) in boundaries" :key="'bd'+i">
          <line
            :x1="cxBoundary(b.seq)" :x2="cxBoundary(b.seq)"
            :y1="m.t" :y2="m.t + chartH"
            stroke="#888" stroke-width="1" stroke-dasharray="3 3"
          />
          <text :x="cxBoundary(b.seq) + 3" :y="m.t + 12" font-size="10" fill="#666">
            重新基准 v{{ b.version }}
          </text>
        </g>

        <!-- 均值折线 -->
        <polyline
          :points="linePoints('mean', scales.x)"
          fill="none" stroke="#2b5d8a" stroke-width="1.4"
        />
        <circle
          v-for="(p, i) in drawnPoints.x" :key="'px'+i"
          :cx="cx(p.seq)" :cy="y(p.value, scales.x)"
          :r="p.r" :fill="p.fill" :stroke="p.stroke" :stroke-width="p.sw"
        >
          <title>{{ p.tip }}</title>
        </circle>

        <text :x="m.l - 46" :y="m.t + chartH/2" font-size="12" fill="#333">均值 X̄</text>
        <g v-for="(tk, i) in xTicks" :key="'xtk'+i">
          <line :x1="cx(tk)" :x2="cx(tk)" :y1="m.t + chartH" :y2="m.t + chartH + 4" stroke="#999"/>
          <text :x="cx(tk)" :y="m.t + chartH + 16" text-anchor="middle" font-size="10" fill="#666">{{ tk }}</text>
        </g>
        <g v-for="(tk, i) in yTicksX" :key="'ytkx'+i">
          <line :x1="m.l" :x2="m.l - 4" :y1="y(tk, scales.x)" :y2="y(tk, scales.x)" stroke="#999"/>
          <text :x="m.l - 6" :y="y(tk, scales.x) + 3" text-anchor="end" font-size="10" fill="#666">{{ fmtTick(tk) }}</text>
        </g>
      </g>

      <!-- R 图 -->
      <g class="chart-r" :transform="`translate(0, ${chartH + gap + 30})`">
        <rect :x="m.l" :y="m.t" :width="plotW" :height="chartH" fill="#fafbfc" />
        <g v-for="(seg, i) in segments.r" :key="'segr'+i">
          <line :x1="cx(seg.from)" :x2="cx(seg.to)"
                :y1="y(seg.ucl, scales.r)" :y2="y(seg.ucl, scales.r)"
                stroke="#b25e00" stroke-width="1.4" stroke-dasharray="6 3"/>
          <line :x1="cx(seg.from)" :x2="cx(seg.to)"
                :y1="y(seg.lcl, scales.r)" :y2="y(seg.lcl, scales.r)"
                stroke="#b25e00" stroke-width="1.4" stroke-dasharray="6 3"/>
          <line :x1="cx(seg.from)" :x2="cx(seg.to)"
                :y1="y(seg.cl, scales.r)" :y2="y(seg.cl, scales.r)"
                stroke="#333" stroke-width="1.2"/>
        </g>
        <polyline :points="linePoints('range', scales.r)" fill="none" stroke="#2b5d8a" stroke-width="1.4"/>
        <circle
          v-for="(p, i) in drawnPoints.r" :key="'pr'+i"
          :cx="cx(p.seq)" :cy="y(p.value, scales.r)"
          :r="p.r" :fill="p.fill" :stroke="p.stroke" :stroke-width="p.sw"
        >
          <title>{{ p.tip }}</title>
        </circle>
        <text :x="m.l - 46" :y="m.t + chartH/2" font-size="12" fill="#333">极差 R</text>
        <g v-for="(tk, i) in xTicks" :key="'xtkr'+i">
          <line :x1="cx(tk)" :x2="cx(tk)" :y1="m.t + chartH" :y2="m.t + chartH + 4" stroke="#999"/>
          <text :x="cx(tk)" :y="m.t + chartH + 16" text-anchor="middle" font-size="10" fill="#666">{{ tk }}</text>
        </g>
        <g v-for="(tk, i) in yTicksR" :key="'ytkr'+i">
          <line :x1="m.l" :x2="m.l - 4" :y1="y(tk, scales.r)" :y2="y(tk, scales.r)" stroke="#999"/>
          <text :x="m.l - 6" :y="y(tk, scales.r) + 3" text-anchor="end" font-size="10" fill="#666">{{ fmtTick(tk) }}</text>
        </g>
      </g>

      <!-- 图例 -->
      <g :transform="`translate(${m.l}, ${geom.height - 8})`" font-size="11" fill="#555">
        <circle cx="6" cy="-4" r="4" fill="#4a90d9"/>
        <text x="16" y="0">受控点</text>
        <circle cx="80" cy="-4" r="5" fill="#d12f2f"/>
        <text x="90" y="0">告警触发点</text>
        <circle cx="180" cy="-4" r="5" fill="#fff" stroke="#d12f2f" stroke-width="2"/>
        <text x="190" y="0">告警涉及点</text>
        <circle cx="280" cy="-4" r="4" fill="#c4ccd4"/>
        <text x="290" y="0">基准期参考点（不参与判异）</text>
        <line x1="430" y1="-4" x2="460" y2="-4" stroke="#b25e00" stroke-dasharray="6 3"/>
        <text x="466" y="0">控制限（后端冻结）</text>
      </g>
    </svg>
    <p v-else class="muted">还没有子组，录入测量值后此处显示控制图。</p>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({ series: { type: Object, required: true } })

const width = 980
const chartH = 250
const gap = 10
const m = { l: 64, r: 70, t: 24, b: 26 }
const height = (m.t + chartH + m.b) * 2 + gap + 24
const plotW = width - m.l - m.r
const geom = computed(() => (props.series.points.length ? { width, height } : null))

const maxSeq = computed(() => {
  const ps = props.series.points
  return ps.length ? ps[ps.length - 1].seq : 0
})

function cx(seq) {
  return m.l + ((seq - 1) / Math.max(1, maxSeq.value - 1)) * plotW
}
function cxBoundary(seq) {
  // 分界画在 seq 与 seq+1 中间
  return m.l + ((seq - 0.5) / Math.max(1, maxSeq.value - 1)) * plotW
}
function y(v, sc) {
  if (sc.max === sc.min) return m.t + chartH / 2
  return m.t + chartH - ((v - sc.min) / (sc.max - sc.min)) * chartH
}

// 每条基准在图上画成一个限段：[lineFrom, lineTo ?? maxSeq]
const limitBases = computed(() => props.series.baselines || [])

const segments = computed(() => {
  const mk = (key) =>
    limitBases.value.map((b) => ({
      version: b.version,
      from: Math.max(1, b.lineFrom),
      to: b.lineTo ?? maxSeq.value,
      ucl: b[key === 'x' ? 'uclX' : 'uclR'],
      cl: b[key === 'x' ? 'xbarBar' : 'rbar'],
      lcl: b[key === 'x' ? 'lclX' : 'lclR']
    })).filter((s) => s.from <= maxSeq.value)
  return { x: mk('x'), r: mk('r') }
})

const boundaries = computed(() =>
  limitBases.value
    .filter((b) => b.version > 1)
    .map((b) => ({ seq: b.effectiveFrom - 1, version: b.version }))
    .filter((b) => b.seq >= 1 && b.seq < maxSeq.value)
)

const specLines = computed(() => {
  const t = props.series.target
  const out = { x: [], r: [] }
  if (t.usl != null) out.x.push({ value: t.usl, label: 'USL' })
  if (t.lsl != null) out.x.push({ value: t.lsl, label: 'LSL' })
  return out
})

// 告警按点归类：触发点 / 涉及点
const pointAlarms = computed(() => {
  const map = new Map()
  for (const a of props.series.alarms) {
    if (!map.has(a.triggerSeq)) map.set(a.triggerSeq, { trigger: [], involved: new Set() })
    map.get(a.triggerSeq).trigger.push(a.rule)
    for (const s of a.involvedSeq) {
      if (!map.has(s)) map.set(s, { trigger: [], involved: new Set() })
      map.get(s).involved.add(a.rule)
    }
  }
  return map
})

const baselineVersion = computed(() => {
  const m2 = new Map()
  for (const b of limitBases.value) if (b.id != null) m2.set(b.id, b.version)
  return m2
})

function styleFor(p, valueKey) {
  const al = pointAlarms.value.get(p.seq)
  let fill = '#4a90d9'
  let stroke = '#2b5d8a'
  let sw = 1
  let r = 4
  if (p.baselineId == null) {
    fill = '#c4ccd4'
    stroke = '#8a94a0'
  }
  if (al) {
    if (al.involved.size) { stroke = '#d12f2f'; sw = 2; fill = '#ffffff' }
    if (al.trigger.length) { fill = '#d12f2f'; stroke = '#8c1d1d'; r = 5.5 }
  }
  const ver = p.baselineId == null ? '基准期参考' : `限版本 v${baselineVersion.value.get(p.baselineId)}`
  const alarmTxt = al
    ? `｜告警规则：${[...new Set([...al.trigger, ...al.involved])].sort().join('、')}`
    : ''
  const tip = `第 ${p.seq} 组｜${ver}\n均值=${p.mean}｜极差=${p.range}\n测量值=${p.values.join(', ')}${alarmTxt}`
  return { seq: p.seq, value: p[valueKey], fill, stroke, sw, r, tip }
}

const drawnPoints = computed(() => ({
  x: props.series.points.map((p) => styleFor(p, 'mean')),
  r: props.series.points.map((p) => styleFor(p, 'range'))
}))

function linePoints(key, sc) {
  return props.series.points
    .map((p) => `${cx(p.seq).toFixed(1)},${y(p[key], sc).toFixed(1)}`)
    .join(' ')
}

// y 量程：点 + 各限段 + 规格线（仅 Xbar）
function rangeFor(valueKey, segs, extra) {
  let min = Infinity
  let max = -Infinity
  for (const p of props.series.points) {
    min = Math.min(min, p[valueKey])
    max = Math.max(max, p[valueKey])
  }
  for (const s of segs) {
    min = Math.min(min, s.ucl, s.lcl, s.cl)
    max = Math.max(max, s.ucl, s.lcl, s.cl)
  }
  for (const e of extra) { min = Math.min(min, e.value); max = Math.max(max, e.value) }
  if (!isFinite(min)) { min = 0; max = 1 }
  if (min === max) { min -= 1; max += 1 }
  const pad = (max - min) * 0.08
  return { min: min - pad, max: max + pad }
}

const scales = computed(() => ({
  x: rangeFor('mean', segments.value.x, specLines.value.x),
  r: rangeFor('range', segments.value.r, [])
}))

// 刻度
function ticks(sc) {
  const out = []
  const steps = 5
  for (let i = 0; i <= steps; i++) out.push(sc.min + ((sc.max - sc.min) * i) / steps)
  return out
}
const yTicksX = computed(() => ticks(scales.value.x))
const yTicksR = computed(() => ticks(scales.value.r))
const xTicks = computed(() => {
  const n = maxSeq.value
  if (!n) return []
  const want = Math.min(20, n)
  const step = Math.max(1, Math.ceil(n / want))
  const out = []
  for (let s = 1; s <= n; s += step) out.push(s)
  if (out[out.length - 1] !== n) out.push(n)
  return out
})

function fmtTick(v) {
  return Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')
}
</script>
