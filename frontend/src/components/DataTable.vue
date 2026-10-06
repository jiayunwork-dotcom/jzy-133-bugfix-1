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
            <th>适用限</th>
            <th>告警</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in rows" :key="p.seq" :class="{ alarmRow: p.rules.length }">
            <td>{{ p.seq }}</td>
            <td v-for="i in series.subgroupN" :key="i">{{ fmt(p.values[i-1]) }}</td>
            <td>{{ fmt(p.mean) }}</td>
            <td>{{ fmt(p.range) }}</td>
            <td>{{ p.baselineId ? ('v' + versionOf(p.baselineId)) : '基准期' }}</td>
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
