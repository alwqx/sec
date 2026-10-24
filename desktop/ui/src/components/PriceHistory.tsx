import { useCallback } from 'react'
import { history } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import PriceChart from './PriceChart'

function LoadedHistory({ code }: { code: string }) {
  const load = useCallback((signal: AbortSignal) => history(code, signal), [code])
  const resource = useResource(load)
  const values = resource.data?.filter(item => Number.isFinite(item.close)) ?? []
  return <div className="history-section"><ResourceState {...resource} retry={resource.reload} />
    {resource.data && (values.length ? <PriceChart data={values} title="近 60 个交易日 · 收盘价 · 不复权" unit={code.startsWith('HK') ? '港元' : '元'} label={`${code} 收盘价走势`} /> : <p className="data-note">暂无历史行情</p>)}
  </div>
}

export default function PriceHistory({ code }: { code: string }) {
  return /^(SH|SZ)\d{6}$|^HK\d{5}$/i.test(code) ? <LoadedHistory code={code} /> : <p className="data-note">走势图暂支持沪深证券与港股。</p>
}
