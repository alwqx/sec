import { useCallback } from 'react'
import { usCompany } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'

const fixed = (value: number | null | undefined, digits = 2) => value === null || value === undefined ? '—' : value.toFixed(digits)
// 亿美元、亿股显示大数。
const yi = (value: number | null | undefined, unit: string) => value === null || value === undefined ? '—' : `${(value / 1e8).toLocaleString('zh-CN', { maximumFractionDigits: 2 })} 亿${unit}`

// USCompany 是美股的公司栏：SEC EDGAR 公司资料和新浪行情估值。
export default function USCompany({ code }: { code: string }) {
  const load = useCallback((signal: AbortSignal) => usCompany(code, signal), [code])
  const resource = useResource(load)
  const data = resource.data
  const company = data?.info.company
  const quote = data?.info.quote
  const fiscal = company?.fiscal_year_end?.length === 4 ? `${company.fiscal_year_end.slice(0, 2)}-${company.fiscal_year_end.slice(2)}` : ''
  const rows: [string, string][] = company ? [
    ['公司名称', company.name], ['SEC CIK', String(company.cik)], ['交易所', company.exchanges?.join('/') ?? ''],
    ['行业', company.industry + (company.sic ? `（SIC ${company.sic}）` : '')], ['注册地', company.state_of_incorporation],
    ['申报人类别', company.category], ['财年截至', fiscal], ['办公地址', company.business_address], ['电话', company.phone], ['公司网址', company.website],
    ['曾用名', company.former_names?.map(name => `${name.name}（${[name.from, name.to].filter(Boolean).join(' 至 ')}）`).join('；') ?? ''],
  ] : []
  return <section className="research-section">
    <ResourceState {...resource} retry={resource.reload} />
    {data?.notes.map(note => <p key={note} className="data-note">{note}</p>)}
    {quote && <dl className="quote-grid">{[
      ['当前价格', `${fixed(quote.price)} 美元`], ['涨跌幅', `${quote.change_rate > 0 ? '+' : ''}${fixed(quote.change_rate)}%`],
      ['市盈率 TTM', fixed(quote.pe)], ['每股收益 TTM', quote.eps === null ? '—' : `${fixed(quote.eps)} 美元`],
      ['总市值', yi(quote.market_cap, '美元')], ['总股本', yi(quote.shares, '股')],
      ['52 周区间', quote.low_52w && quote.high_52w ? `${fixed(quote.low_52w)} - ${fixed(quote.high_52w)}` : '—'], ['行情时间', `${quote.time} 北京时间`],
    ].map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>}
    {company && <div className="table-scroll"><table className="data-table us-company-table"><tbody>
      {rows.map(([label, value]) => <tr key={label}><th scope="row">{label}</th><td>{value || '—'}</td></tr>)}
    </tbody></table></div>}
    <p className="data-note">公司资料来自 SEC EDGAR，行情和估值来自新浪；每股收益和市盈率按最近四个季度计算，亏损公司没有市盈率。</p>
  </section>
}
