// 与后端交互的唯一通道。前端不计算控制限/能力指数，全部数字取自这里。

async function request(path, options = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(body.error || `请求失败（HTTP ${res.status}）`)
  }
  return body
}

export const api = {
  listTargets: () => request('/api/targets'),
  createTarget: (payload) =>
    request('/api/targets', { method: 'POST', body: JSON.stringify(payload) }),
  getSeries: (id) => request(`/api/targets/${id}/series`),
  ingest: (id, values, grouped) =>
    request(`/api/targets/${id}/ingest`, {
      method: 'POST',
      body: JSON.stringify({ values, grouped })
    }),
  createBaseline: (id, startSeq, endSeq) =>
    request(`/api/targets/${id}/baselines`, {
      method: 'POST',
      body: JSON.stringify({ startSeq, endSeq })
    })
}
