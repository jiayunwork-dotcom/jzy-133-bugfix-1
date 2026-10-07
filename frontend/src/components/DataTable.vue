<template>
  <div class="panel">
    <h2>子组数据（共 {{ series.points.length }} 组）</h2>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>组号</th>
            <th v-for="i in series.subgroupN" :key="i">x{{ i }}</th>
            <th>均值 X̄</th>
            <th>极差 R</th>
            <th>判定限</th>
            <th>参考期</th>
            <th>告警</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in rows" :key="p.seq" :class="{ alarmRow: p.rules.length }">
            <td>{{ p.seq }}</td>
            <td v-for="i in series.subgroupN" :key="i">{{ fmt(p.values[i-1]) }}</td>
            <td>{{ fmt(p.mean) }}</td>
            <td>{{ fmt(p.range) }}</td>
            <td>{{ limitLabel(p) }}</td>
            <td>{{ refLabel(p) }}</td>
            <td class="alarm-cell">{{ p.rules.length ? '规则' + p.rules.join('、') : '' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({ series: { type: Object, required: true } })

const versionOf = (id) => {
  const b = props.series.baselines.find((x) => x.id === id)
  return b ? b.version : '?'
}

// 判定限：点切出那一刻归属的限（落库不变）。没有则是基准期/基准前的未判定点。
function limitLabel(p) {
  if (p.baselineId == null) {
    return p.refBaselineId == null ? '未判定' : '基准期参考'
  }
  return 'v' + versionOf(p.baselineId)
}

// 参考期：该点是否又被后来的某版限选作参考（展示属性，不改判定归属）。
function refLabel(p) {
  if (p.refBaselineId == null) return ''
  const v = versionOf(p.refBaselineId)
  return p.baselineId == null ? `v${v} 参考` : `兼 v${v} 参考`
}

const rows = computed(() => {
  const rulesAt = new Map()
  for (const a of props.series.alarms) {
    const all = new Set(a.involvedSeq)
    all.add(a.triggerSeq)
    for (const s of all) {
      if (!rulesAt.has(s)) rulesAt.set(s, new Set())
      rulesAt.get(s).add(a.rule)
    }
  }
  return props.series.points.map((p) => ({
    ...p,
    rules: [...(rulesAt.get(p.seq) || [])].sort((a, b) => a - b)
  }))
})

function fmt(v) {
  if (v == null) return ''
  return Number(v).toFixed(4).replace(/0+$/, '').replace(/\.$/, '')
}
</script>

<style scoped>
tr.alarmRow { background: #fdf3f3; }
</style>
