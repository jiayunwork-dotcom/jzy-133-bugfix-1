<template>
  <div class="panel">
    <h2>录入测量值 <span class="muted">（n={{ series.subgroupN }}）</span></h2>

    <div class="row" style="margin-bottom:8px">
      <label class="checkbox">
        <input type="radio" :value="false" v-model="grouped" />
        自动分组（逐个输入 / 粘贴一列，按服务器点序每 {{ series.subgroupN }} 个切一组）
      </label>
    </div>
    <div class="row" style="margin-bottom:8px">
      <label class="checkbox">
        <input type="radio" :value="true" v-model="grouped" />
        整组提交（本次恰好 {{ series.subgroupN }} 个值，单独成一组）
      </label>
    </div>

    <div v-if="grouped" class="row">
      <div v-for="i in series.subgroupN" :key="i" style="flex:1;min-width:90px">
        <label class="muted">第 {{ i }} 个</label>
        <input v-model="slots[i-1]" inputmode="decimal" />
      </div>
    </div>
    <div v-else>
      <textarea
        v-model="text"
        rows="5"
        :placeholder="`粘贴一列测量值，支持空格/逗号/换行分隔，例如：
10.02 10.05 9.98 ...（任意数量，凑满 ${series.subgroupN} 个自动切组）`"
      ></textarea>
    </div>

    <div v-if="error" class="error">{{ error }}</div>
    <div v-if="info" class="success">{{ info }}</div>

    <div class="row" style="margin-top:8px">
      <button class="primary" :disabled="submitting" @click="submit">
        {{ submitting ? '提交中…' : '提交' }}
      </button>
      <button @click="clear">清空</button>
      <span v-if="series.pending" class="muted">
        注意：当前已有 {{ series.pending }} 个未凑满散点，自动分组会先与它们拼组。
      </span>
    </div>

    <div v-if="lastResult" style="margin-top:10px">
      <div class="muted">
        本次新切子组 {{ lastResult.newSubgroups.length }} 个，
        待成组散点 {{ lastResult.pendingAfter }} 个
      </div>
      <div v-if="lastResult.newAlarms.length" class="error">
        新告警：
        <span v-for="(a, i) in lastResult.newAlarms" :key="i">
          第{{ a.triggerSeq }}组·规则{{ a.rule }}；
        </span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { api } from '../api/client'
import { parseMeasurements } from '../validation'

const props = defineProps({ series: { type: Object, required: true } })
const emit = defineEmits(['ingested'])

const grouped = ref(false)
const text = ref('')
const slots = ref(Array(props.series.subgroupN).fill(''))
// 切换监控对象（容量可能变化）时重建整组输入框。
watch(
  () => props.series.target.id,
  () => { slots.value = Array(props.series.subgroupN).fill('') }
)
const error = ref('')
const info = ref('')
const submitting = ref(false)
const lastResult = ref(null)

function clear() {
  text.value = ''
  slots.value = Array(props.series.subgroupN).fill('')
  error.value = ''
  info.value = ''
}

async function submit() {
  error.value = ''
  info.value = ''
  let values
  if (grouped.value) {
    const filled = slots.value.map((s) => s.trim())
    if (filled.some((s) => s === '')) {
      error.value = `整组提交需填满 ${props.series.subgroupN} 个输入框`
      return
    }
    const parsed = parseMeasurements(filled.join(' '))
    if (parsed.error) { error.value = parsed.error; return }
    if (parsed.values.length !== props.series.subgroupN) {
      error.value = `子组容量与档案不符：本次 ${parsed.values.length} 个，档案要求 ${props.series.subgroupN} 个`
      return
    }
    values = parsed.values
  } else {
    const parsed = parseMeasurements(text.value)
    if (parsed.error) { error.value = parsed.error; return }
    values = parsed.values
  }

  submitting.value = true
  try {
    const res = await api.ingest(props.series.target.id, values, grouped.value)
    lastResult.value = res
    info.value = '提交成功'
    clear()
    emit('ingested')
  } catch (e) {
    error.value = e.message
  } finally {
    submitting.value = false
  }
}
</script>
