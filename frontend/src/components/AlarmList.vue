<template>
  <div class="panel">
    <h2>判异告警 <span class="muted">（共 {{ alarms.length }} 条，只增不删）</span></h2>
    <div v-if="!alarms.length" class="success">当前无告警</div>
    <div v-else class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="text-align:left">规则</th>
            <th>触发子组</th>
            <th style="text-align:left">涉及子组</th>
            <th>适用限版本</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(a, i) in alarms" :key="i">
            <td class="alarm-cell" style="text-align:left">
              规则{{ a.rule }}：{{ a.ruleName }}
            </td>
            <td class="alarm-cell">第 {{ a.triggerSeq }} 组</td>
            <td style="text-align:left">{{ formatInvolved(a.involvedSeq) }}</td>
            <td>{{ a.baselineId ? ('#' + a.baselineId) : '—' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
defineProps({ alarms: { type: Array, required: true } })

// 把连续序号压缩成区间，如 3,4,5,6,7,8 -> 3~8
function formatInvolved(seq) {
  if (!seq || !seq.length) return '—'
  const ranges = []
  let start = seq[0]
  let prev = seq[0]
  for (let i = 1; i <= seq.length; i++) {
    if (i < seq.length && seq[i] === prev + 1) {
      prev = seq[i]
      continue
    }
    ranges.push(start === prev ? `${start}` : `${start}~${prev}`)
    start = seq[i]
    prev = seq[i]
  }
  return ranges.join('，')
}
</script>
