import { useCallback, useMemo } from 'react'
import { history } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import CandlestickChart from './CandlestickChart'

function LoadedHistory({ code }: { code: string }) {
  const load = useCallback((signal: AbortSignal) => history(code, signal), [code])
  const resource = useResource(load)
  // 保持历史数据引用稳定，避免实时行情刷新时触发 K 线自动滚回最新交易日。
  const values = useMemo(() => resource.data?.filter(item =>
    [item.open, item.close, item.high, item.low].every(Number.isFinite),
  ) ?? [], [resource.data])
  return <div className="history-section"><ResourceState {...resource} retry={resource.reload} />
    {resource.data && (values.length ? <>
      <p className="data-note">近 60 个交易日 · 不复权</p>
      <CandlestickChart data={values} name={code} showVolume unit={code.startsWith('HK') ? '港元' : '元'} />
    </> : <p className="data-note">暂无历史行情</p>)}
  </div>
}

export default function PriceHistory({ code }: { code: string }) {
  return /^(SH|SZ)\d{6}$|^HK\d{5}$/i.test(code) ? <LoadedHistory code={code} /> : <p className="data-note">走势图暂支持沪深证券与港股。</p>
}
