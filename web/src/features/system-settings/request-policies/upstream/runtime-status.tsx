import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { toIntlLocale } from '@/i18n/languages'
import { formatLocalCurrencyAmount } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import { policyLabel } from '../policy-label'
import type { Runtime, UpstreamAccount } from './api'

export function RuntimeStatus(props: {
  state?: Runtime
  paused?: boolean
  account?: UpstreamAccount
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const state = props.state
  const now = Math.floor(Date.now() / 1000)
  let status = t('Ready')
  if (!state) status = t('No traffic yet')
  if (state?.recovering) status = t('Recovery probe')
  if ((state?.cooldown_until ?? 0) > now) status = t('Cooling down')
  if (state?.paused) status = policyLabel(t, state.paused)
  if (props.paused) status = t('Account paused')
  const inFlight = Object.values(state?.leases ?? {}).filter(
    (l) => l.expires > now
  ).length
  const requests = state?.events?.filter((e) => e.at > now - 60).length ?? 0
  const budget = props.account?.budget
  const balance = props.account?.balance
  return (
    <div className='space-y-1'>
      <Badge variant='secondary'>{status}</Badge>
      {state && (
        <p className='text-muted-foreground text-xs'>
          {t('Active')}: {formatNumber(inFlight, locale)} · RPM:{' '}
          {formatNumber(requests, locale)} · {t('Success / error')}:{' '}
          {formatNumber(state.successes, locale)} /{' '}
          {formatNumber(state.errors, locale)}
        </p>
      )}
      {(state?.cooldown_until ?? 0) > now && (
        <p className='text-xs'>
          {t('Retry in seconds')}:{' '}
          {formatNumber((state?.cooldown_until ?? 0) - now, locale)}
        </p>
      )}
      {props.account && (
        <p className='text-muted-foreground text-xs'>
          {t('Upstream budget')}:{' '}
          {budget && budget.amount > 0
            ? `${formatNumber(state?.usage?.[budget.unit as 'requests' | 'tokens' | 'usd'] ?? 0, locale)} / ${formatNumber(budget.amount, locale)} ${budget.unit}`
            : t('Unknown')}{' '}
          · {t('Upstream balance')}:{' '}
          {balance == null
            ? t('Unknown')
            : `${formatLocalCurrencyAmount(Math.max(0, balance - (state?.balance_used ?? 0)), { showSymbol: false, abbreviate: false, digitsSmall: 6, locale })} USD (${t('Manual')})`}
        </p>
      )}
      {state?.estimated && (
        <p className='text-muted-foreground text-xs'>
          {t('Includes estimated or reserved usage')}
        </p>
      )}
    </div>
  )
}
