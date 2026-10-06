// 前端表单校验，与后端 domain.ValidateTarget / 录入校验保持一致。
// 前端仅用于即时提示，后端是最终裁决。

export const RULES = [
  { no: 1, label: '规则1：一点超出三倍标准差' },
  { no: 2, label: '规则2：连续九点落在中心线同侧' },
  { no: 3, label: '规则3：连续六点递增或递减' },
  { no: 4, label: '规则4：三点中有两点落在同侧 2σ~3σ 之间' }
]

export function validateTargetForm(f) {
  const problems = []
  if (!f.name.trim()) problems.push('监控对象名称不能为空')
  if (!f.machine.trim()) problems.push('机床不能为空')
  if (!f.dimension.trim()) problems.push('尺寸名称不能为空')

  const hasU = f.usl !== null && f.usl !== '' && !Number.isNaN(Number(f.usl))
  const hasL = f.lsl !== null && f.lsl !== '' && !Number.isNaN(Number(f.lsl))
  if (!hasU && !hasL) problems.push('规格上下限至少填写一侧')
  if ((f.usl !== null && f.usl !== '' && Number.isNaN(Number(f.usl))) ||
      (f.lsl !== null && f.lsl !== '' && Number.isNaN(Number(f.lsl)))) {
    problems.push('规格限必须是数字')
  }
  if (hasU && hasL && Number(f.usl) <= Number(f.lsl)) {
    problems.push(`规格上限(${f.usl})必须大于规格下限(${f.lsl})`)
  }
  const n = Number(f.subgroupN)
  if (!Number.isInteger(n) || n < 2 || n > 10) {
    problems.push('子组容量必须是 2 到 10 之间的整数')
  }
  return problems
}

// 解析录入框：支持空格、逗号、制表、换行分隔；返回 { values, error }。
export function parseMeasurements(text) {
  const tokens = text.split(/[\s,，;；]+/).filter((s) => s.length > 0)
  if (tokens.length === 0) {
    return { values: [], error: '请输入至少一个测量值' }
  }
  const values = []
  for (let i = 0; i < tokens.length; i++) {
    const v = Number(tokens[i])
    if (!Number.isFinite(v)) {
      return { values: [], error: `第 ${i + 1} 个值「${tokens[i]}」不是有效数字` }
    }
    values.push(v)
  }
  return { values, error: null }
}

export function validateBaselineRange(start, end, available) {
  const s = Number(start)
  const e = Number(end)
  if (!Number.isInteger(s) || s < 1) return '基准期起始序号必须是 >= 1 的整数'
  if (!Number.isInteger(e) || e < s) return '基准期结束序号不能早于起始序号'
  if (e > available) return `基准期结束子组(第${e}组)超出已有数据(当前共${available}组)`
  if (e - s + 1 < 20) return `基准期至少需要 20 个子组，当前仅 ${e - s + 1} 个`
  return null
}
