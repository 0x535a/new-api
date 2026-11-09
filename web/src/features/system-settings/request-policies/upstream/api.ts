import { z } from 'zod'

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

const nonnegative = z.number().finite().min(0)
export const limitsSchema = z.object({
  rpm: nonnegative.max(100000).int(),
  tpm: nonnegative.max(1e12).int(),
  concurrency: nonnegative.max(100000).int(),
})
export const accountSchema = z
  .object({
    id: z.string().regex(/^[a-zA-Z0-9_-]{1,64}$/),
    name: z.string().trim().min(1).max(128),
    channel_ids: z.array(z.number().int().positive()).min(1),
    paused: z.boolean(),
    limits: limitsSchema,
    budget: z.object({
      amount: nonnegative.max(1e12),
      reserve: nonnegative.max(1e12),
      unit: z.enum(['', 'requests', 'tokens', 'usd']),
      period: z.enum(['', 'day', 'month', 'lifetime']),
      timezone: z.string(),
    }),
    balance: nonnegative.nullable(),
    balance_source: z.string(),
    balance_updated_at: nonnegative,
  })
  .refine((a) => a.budget.reserve <= a.budget.amount, {
    path: ['budget.reserve'],
    message: 'Reserve exceeds budget',
  })
export const sourceSchema = z.object({
  channel_id: z.number().int().positive(),
  model: z.string().max(255),
  enabled: z.boolean(),
  verified: z.boolean(),
  priority: nonnegative.int().max(100000),
  weight: z.number().int().min(1).max(100000),
  cost_tier: nonnegative.int().max(1000),
  cost_known: z.boolean(),
  limits: limitsSchema,
  input_usd_per_million: nonnegative.max(1e6),
  output_usd_per_million: nonnegative.max(1e6),
  request_usd: nonnegative.max(1e6),
})
export const configSchema = z.object({
  revision: nonnegative.int(),
  enabled: z.boolean(),
  max_attempts: z.number().int().min(1).max(3),
  failure_threshold: z.number().int().min(1).max(20),
  cooldown_seconds: z.number().int().min(1).max(300),
  accounts: z.array(accountSchema),
  models: z.array(
    z.object({
      model: z.string().trim().min(1).max(255),
      enabled: z.boolean(),
      sources: z.array(sourceSchema),
    })
  ),
})
export type RoutingConfig = z.infer<typeof configSchema>
export type UpstreamAccount = z.infer<typeof accountSchema>
export type UpstreamSource = z.infer<typeof sourceSchema>
export type ModelPool = RoutingConfig['models'][number]
export type Usage = { requests: number; tokens: number; usd: number }
export type Runtime = {
  period: string
  usage: Usage
  lifetime: Usage
  events: { at: number; tokens: number }[] | null
  leases: Record<string, { expires: number }> | null
  paused: string
  cooldown_until: number
  recovering: boolean
  successes: number
  errors: number
  estimated: boolean
  balance_used: number
}
export type Channel = {
  id: number
  name: string
  models: string
  status: number
  tag: string | null
}
export type RoutingData = {
  config: RoutingConfig
  states: Record<string, Runtime>
  channels: Channel[]
  source_scopes: Record<string, Record<number, string>>
}
type Response<T> = { success: boolean; message?: string; data: T }
export const routingQueryKey = ['upstream-routing']
export async function getRouting() {
  const r = await api.get<Response<RoutingData>>(
    '/api/channel/upstream_routing'
  )
  return requireServerSuccess(r.data).data
}
export async function saveRouting(config: RoutingConfig) {
  const r = await api.put<Response<RoutingConfig>>(
    '/api/channel/upstream_routing',
    config
  )
  return requireServerSuccess(r.data).data
}
export async function operateRouting(
  scope: string,
  action: 'recover' | 'reset_budget'
) {
  const r = await api.post<Response<unknown>>(
    '/api/channel/upstream_routing/operate',
    { scope, action }
  )
  return requireServerSuccess(r.data)
}
export const emptyLimits = () => ({ rpm: 0, tpm: 0, concurrency: 0 })
export function newAccount(): UpstreamAccount {
  return {
    id: '',
    name: '',
    channel_ids: [],
    paused: false,
    limits: emptyLimits(),
    budget: {
      amount: 0,
      reserve: 0,
      unit: '',
      period: '',
      timezone: 'Asia/Singapore',
    },
    balance: null,
    balance_source: '',
    balance_updated_at: 0,
  }
}
export function newSource(channelID: number): UpstreamSource {
  return {
    channel_id: channelID,
    model: '',
    enabled: false,
    verified: false,
    priority: 0,
    weight: 1,
    cost_tier: 0,
    cost_known: false,
    limits: emptyLimits(),
    input_usd_per_million: 0,
    output_usd_per_million: 0,
    request_usd: 0,
  }
}
export function isGPT(name: string) {
  return /^(gpt-|chatgpt-|codex-|o1|o3|o4|chat-latest$)/i.test(
    name.split('/').at(-1) ?? name
  )
}
// Directory imports are deliberately pending. A newly advertised model must
// not inherit verification from another model on the same channel.
export function stageDirectory(
  config: RoutingConfig,
  channels: Channel[]
): RoutingConfig {
  const next = structuredClone(config)
  const accountChannels = new Set(next.accounts.flatMap((a) => a.channel_ids))
  for (const channel of channels) {
    if (!accountChannels.has(channel.id) || channel.tag === 'GPT') continue
    for (const name of channel.models
      .split(',')
      .map((m) => m.trim())
      .filter(Boolean)) {
      if (isGPT(name)) continue
      let pool = next.models.find((p) => p.model === name)
      if (!pool) {
        pool = { model: name, enabled: false, sources: [] }
        next.models.push(pool)
      }
      if (!pool.sources.some((s) => s.channel_id === channel.id)) {
        pool.sources.push(newSource(channel.id))
      }
    }
  }
  return next
}
