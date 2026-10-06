<template>
  <div>
    <h2 style="margin-top:0">监控对象</h2>
    <button class="primary" style="width:100%;margin-bottom:12px" @click="showForm = !showForm">
      {{ showForm ? '收起' : '新建监控对象' }}
    </button>

    <div v-if="showForm" class="panel" style="padding:12px;margin-bottom:12px;background:#fafbfc">
      <div class="field">
        <label>名称 *</label>
        <input v-model="form.name" placeholder="如：主轴外圆 Φ20" />
      </div>
      <div class="field">
        <label>机床 *</label>
        <input v-model="form.machine" placeholder="如：CNC-07" />
      </div>
      <div class="field">
        <label>尺寸 *</label>
        <input v-model="form.dimension" placeholder="如：外径" />
      </div>
      <div class="row">
        <div class="field" style="flex:1">
          <label>规格上限 USL（单侧可留空）</label>
          <input v-model="form.usl" placeholder="留空=无上限" />
        </div>
        <div class="field" style="flex:1">
          <label>规格下限 LSL（单侧可留空）</label>
          <input v-model="form.lsl" placeholder="留空=无下限" />
        </div>
      </div>
      <div class="field">
        <label>子组容量（2~10）</label>
        <input v-model.number="form.subgroupN" type="number" min="2" max="10" step="1" />
      </div>
      <div class="field">
        <label>启用判异规则</label>
        <label v-for="r in RULES" :key="r.no" class="checkbox">
          <input type="checkbox" :value="r.no" v-model="form.enabledRules" />
          {{ r.label }}
        </label>
      </div>
      <div v-if="formError" class="error">{{ formError }}</div>
      <button class="primary" @click="submit">建档</button>
    </div>

    <div v-if="loading" class="muted">加载中…</div>
    <div
      v-for="t in targets"
      :key="t.id"
      class="target-item"
      :class="{ active: t.id === currentId }"
      @click="$emit('select', t.id)"
    >
      <div class="name">{{ t.name }}</div>
      <div class="meta">{{ t.machine }} · {{ t.dimension }} · n={{ t.subgroupN }}</div>
    </div>
    <div v-if="!loading && targets.length === 0" class="muted">还没有监控对象</div>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { api } from '../api/client'
import { RULES, validateTargetForm } from '../validation'

defineProps({
  targets: { type: Array, required: true },
  currentId: { type: [Number, null], default: null },
  loading: Boolean
})
const emit = defineEmits(['select', 'created'])

const showForm = ref(false)
const formError = ref('')
const blank = () => ({
  name: '', machine: '', dimension: '',
  usl: '', lsl: '', subgroupN: 5,
  enabledRules: [1, 2, 3, 4]
})
const form = reactive(blank())

async function submit() {
  formError.value = ''
  const problems = validateTargetForm(form)
  if (problems.length) {
    formError.value = problems.join('\n')
    return
  }
  const payload = {
    name: form.name.trim(),
    machine: form.machine.trim(),
    dimension: form.dimension.trim(),
    subgroupN: Number(form.subgroupN),
    enabledRules: form.enabledRules,
  }
  payload.usl = form.usl === '' ? null : Number(form.usl)
  payload.lsl = form.lsl === '' ? null : Number(form.lsl)
  try {
    const t = await api.createTarget(payload)
    Object.assign(form, blank())
    showForm.value = false
    emit('created', t.id)
  } catch (e) {
    formError.value = e.message
  }
}
</script>
