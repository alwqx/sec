import { useCallback } from 'react'
import { hkCompany } from '../api'
import { money } from '../format'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'

const fixed = (value: number | null | undefined) => value === null || value === undefined ? '—' : value.toFixed(2)
// 港股有港元、人民币和美元柜台，价格和市值按交易币种标注；发行价来自东方财富，是港元。
const currencyName: Record<string, string> = { HKD: '港元', CNY: '元', USD: '美元' }

// HKCompany 是港股的概览栏：东方财富公司资料和估值，新浪价格和市值。
export default function HKCompany({ code }: { code: string }) {
  const load = useCallback((signal: AbortSignal) => hkCompany(code, signal), [code])
  const resource = useResource(load)
  const data = resource.data
  const unit = data ? currencyName[data.currency] ?? data.currency : ''
  const rows: [string, string][] = data?.equity ? [
    ['证券简称', data.short_name], ['所属行业', data.industry], ['上市日期', data.listing_date],
    ['发行价格', data.issue_price === null ? '' : `${fixed(data.issue_price)} 港元`], ['公司网址', data.website],
    ['办公地址', data.business_address], ['注册地址', data.register_address], ['主营业务', data.main_business],
  ] : []
  return <section className="research-section">
    <ResourceState {...resource} retry={resource.reload} />
    {data && <>
      {data.notes.map(note => <p key={note} className="data-note">{note}</p>)}
      {data.equity && <h2>{data.name}</h2>}
      <dl className="quote-grid">{[
        [data.price_is_yclose ? '昨收' : '当前价格', data.price === null ? '—' : `${fixed(data.price)} ${unit}`],
        // 基金、信托、权证没有市盈率、市净率和股本，只显示价格。
        ...(!data.equity ? [] : [
          ['市盈率 TTM', fixed(data.pe_ttm)], ['市净率', fixed(data.pb)],
          ['总市值', money(data.market_cap, unit)], ['流通市值', money(data.float_market_cap, unit)],
        ]),
      ].map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
      {rows.length > 0 && <div className="table-scroll"><table className="data-table us-company-table hk-company-table"><tbody>
        {rows.map(([label, value]) => <tr key={label}><th scope="row">{label}</th><td>{value || '—'}</td></tr>)}
      </tbody></table></div>}
      {data.description && <p className="company-description">{data.description}</p>}
      {data.equity && <p className="data-note">公司资料、市盈率和市净率来自东方财富，价格和市值来自新浪；东方财富已把人民币等报表货币换算成港元，亏损公司没有市盈率。</p>}
    </>}
  </section>
}
