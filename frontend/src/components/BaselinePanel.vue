<template>
  <div class="panel">
    <h2>基准与过程能力</h2>
    <div v-if="!series.baselines.length">
      <p class="muted">
        尚未建立基准。请先录入至少 20 个子组，再指定基准期范围发起基准；
        基准期计算出的中心线与控制限将被冻结，之后新录入的子组只按冻结限判定。
      </p>
    </div>
    <div v-else>
      <table style="margin-bottom:10px">
        <thead>
          <tr>
            <th>版本</th><th>基准期子组</th><th>生效区间</th>
            <th>X̄̄</th><th>R̄</th><th>UCLx</th><th>LCLx</th>
            <th>UCLr</th><th>LCLr</th><th>σ(组内)</th><th>状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in series.baselines" :key="b.id">
            <td>v{{ b.version }}</td>
            <td>{{ b.refStartSeq }}~{{ b.refEndSeq }}</td>
            <td>{{ b.effectiveFrom }}~{{ b.effectiveTo ?? '至今' }}</td>
            <td>{{ f(b.xbarBar) }}</td>
            <td>{{ f(b.rbar) }}</td>
            <td>{{ f(b.uclX) }}</td>
            <td>{{ f(b.lclX) }}</td>
            <td>{{ f(b.uclR) }}</td>
            <td>{{ f(b.lclR) }}</td>
            <td>{{ f(b.sigmaWithin) }}</td>
            <td>
              <span class="badge" :class="b.active ? 'ok' : ''">
                {{ b.active ? '当前生效' : '已留档' }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="cap" style="background:#f7f9fb;border:1px solid var(--border);border-radius:8px;padding:12px">
      <strong>过程能力（按基准期、组内标准差 R̄/d₂）</strong>
      <div class="row" style="margin-top:8px;gap:24px">
        <div>Cp：<strong>{{ cap.cp == null ? '—（单侧规格）' : f(cap.cp) }}</strong></div>
        <div>Cpk：<strong>{{ cap.cpk == null ? '无法计算' : f(cap.cpk) }}</strong></div>
      </div>
      <div v-if="!series.inControl" class="error" style="margin-bottom:0">
        ⚠ 过程不受控，能力指数仅供参考。
      </div>
    </div>

    <div style="margin-top:14px">
      <strong>{{ series.baselines.length ? '重新基准（显式发起，旧限自动留档）' : '发起基准' }}</strong>
      <div class="row" style="margin-top:8px">
        <div style="width:140px">
          <label class="muted">基准期起始组</label>
          <input v-model.number="startSeq" type="number" min="1" placeholder="默认 1" />
        </div>
        <div style="width:140px">
          <label class="muted">基准期结束组</label>
          <input v-model.number="endSeq" type="number" :min="startSeq || 1" />
        </div>
        <button class="primary" style="margin-top:14px" :disabled="busy" @click="submit">
          {{ busy ? '提交中…' : (series.baselines.length ? '重新基准' : '冻结控制限') }}
        </button>
        <span class="muted" style="margin-top:14px">
          当前共 {{ series.points.length }} 个子组；基准期至少 20 个。
        </span>
      </div>
      <div v-if="error" class="error">{{ error }}</div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { api } from '../api/client'
import { validateBaselineRange } from '../validation'

const props = defineProps({ series: { type: Object, required: true } })
const emit = defineEmits(['changed'])

const startSeq = ref(1)
const endSeq = ref('')
const error = ref('')
const busy = ref(false)

const cap = computed(() => props.series.capabilityBaseline)

function f(v) {
  if (v == null) return '—'
  return Number(v).toFixed(5).replace(/0+$/, '').replace(/\.$/, '')
}

async function submit() {
  error.value = ''
  const s = startSeq.value || 1
  const e = endSeq.value
  const err = validateBaselineRange(s, e, props.series.points.length)
  if (err) { error.value = err; return }
  busy.value = true
  try {
    await api.createBaseline(props.series.target.id, Number(s), Number(e))
    endSeq.value = ''
    emit('changed')
  } catch (ex) {
    error.value = ex.message
  } finally {
    busy.value = false
  }
}
</script>
