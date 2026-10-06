<template>
  <div class="app">
    <TargetList
      class="sidebar"
      :targets="targets"
      :current-id="currentId"
      :loading="loadingList"
      @select="selectTarget"
      @created="onTargetCreated"
    />
    <main class="main">
      <div v-if="!series" class="panel">
        <p class="muted" style="font-size:15px">
          请在左侧选择一个监控对象；没有档案时点「新建监控对象」建档。
        </p>
      </div>
      <template v-else>
        <div class="panel">
          <div class="row" style="justify-content:space-between">
            <div>
              <h2 style="margin:0">
                {{ series.target.name }}
                <span class="muted" style="font-weight:400">
                  {{ series.target.machine }} · {{ series.target.dimension }} ·
                  子组容量 n={{ series.subgroupN }}
                </span>
              </h2>
              <div class="muted" style="margin-top:4px">
                规格：
                <template v-if="uslText!==null">USL={{ fmt(uslText) }} </template>
                <template v-else>USL 不限 </template>
                /
                <template v-if="lslText!==null"> LSL={{ fmt(lslText) }}</template>
                <template v-else> LSL 不限</template>
                · 启用规则：{{ series.target.enabledRules.join('、') }}
              </div>
            </div>
            <div>
              <span class="badge" :class="series.inControl ? 'ok' : 'alarm'">
                {{ series.inControl ? '过程受控' : '过程不受控' }}
              </span>
              <span v-if="series.pending" class="badge pending">
                {{ series.pending }} 个散点待成组
              </span>
              <button style="margin-left:10px" @click="refresh">刷新</button>
            </div>
          </div>
        </div>

        <ControlChart :series="series" />
        <AlarmList :alarms="series.alarms" />
        <BaselinePanel
          :series="series"
          @changed="refresh"
        />
        <IngestPanel
          :series="series"
          @ingested="refresh"
        />
        <DataTable :series="series" />
      </template>
    </main>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { api } from './api/client'
import TargetList from './components/TargetList.vue'
import IngestPanel from './components/IngestPanel.vue'
import ControlChart from './components/ControlChart.vue'
import BaselinePanel from './components/BaselinePanel.vue'
import AlarmList from './components/AlarmList.vue'
import DataTable from './components/DataTable.vue'

const targets = ref([])
const series = ref(null)
const currentId = ref(null)
const loadingList = ref(false)

const uslText = computed(() => series.value?.target.usl ?? null)
const lslText = computed(() => series.value?.target.lsl ?? null)

function fmt(v) {
  return Number(v).toFixed(4).replace(/\.?0+$/, '')
}

async function loadTargets(selectId) {
  loadingList.value = true
  try {
    targets.value = await api.listTargets()
    if (selectId) {
      currentId.value = selectId
    } else if (!currentId.value && targets.value.length) {
      currentId.value = targets.value[0].id
    }
    if (currentId.value) await loadSeries()
  } finally {
    loadingList.value = false
  }
}

async function loadSeries() {
  if (!currentId.value) return
  series.value = await api.getSeries(currentId.value)
}

async function selectTarget(id) {
  currentId.value = id
  await loadSeries()
}

async function onTargetCreated(id) {
  await loadTargets(id)
}

async function refresh() {
  await loadSeries()
}

onMounted(() => loadTargets())
</script>
