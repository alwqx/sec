import { useCallback, useRef, useState } from 'react'
import { aiSettings } from '../api'
import { explanationProfile, explanationUnavailable } from '../aiValuation'
import { useResource } from './useResource'
import type { AISettings } from '../types'

// useAIModels 读取模型配置并管理临时选择的配置，摘要列表用。enabled 为 false 时不读取（页面上没有可摘要的条目），
// 不白白请求 /api/v1/ai 和读取系统钥匙串。刷新时保留上一次读到的配置，选中的配置和可用状态不变，
// 不会让各行正在进行或已完成的摘要被重置。
export function useAIModels(enabled = true) {
  const load = useCallback((signal: AbortSignal) => enabled ? aiSettings(signal) : Promise.resolve(null), [enabled])
  const models = useResource(load)
  const last = useRef<AISettings | null>(null)
  if (models.data) last.current = models.data
  const settings = models.data ?? last.current
  const [selected, setSelected] = useState('')
  const profile = explanationProfile(settings, selected)
  const unavailable = models.error ? `读取模型配置失败：${models.error}` : explanationUnavailable(settings)
  return { settings, loading: models.loading, reload: models.reload, profile, unavailable, select: setSelected }
}

export type AIModels = ReturnType<typeof useAIModels>
