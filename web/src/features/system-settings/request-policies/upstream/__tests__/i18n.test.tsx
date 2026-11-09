import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import fr from '@/i18n/locales/fr.json'
import ja from '@/i18n/locales/ja.json'
import ru from '@/i18n/locales/ru.json'
import viLocale from '@/i18n/locales/vi.json'
import zhTW from '@/i18n/locales/zh-TW.json'
import zhCN from '@/i18n/locales/zh.json'

import { AccountDialog } from '../account-dialog'
import { newAccount } from '../api'

const resources = { en, zhCN, zhTW, fr, ru, ja, vi: viLocale }

it.each([
  ['en', 'Upstream load balancing', 'Invalid upstream balance'],
  ['zhCN', '上游负载均衡', '上游余额无效'],
  ['zhTW', '上游負載均衡', '上游餘額無效'],
  ['fr', 'Répartition de charge en amont', 'Solde amont invalide'],
  ['ru', 'Балансировка нагрузки провайдеров', 'Недопустимый баланс провайдера'],
  ['ja', '上流の負荷分散', '上流残高が無効です'],
  ['vi', 'Cân bằng tải nhà cung cấp', 'Số dư nhà cung cấp không hợp lệ'],
])(
  'resolves routing labels and backend errors in %s without fallback',
  async (language, title, error) => {
    const i18n = createInstance()
    await i18n.init({ resources, lng: language, fallbackLng: false })
    expect(i18n.t('Upstream load balancing')).toBe(title)
    expect(i18n.t('Invalid upstream balance')).toBe(error)
    expect(
      i18n.exists(
        'Source limits cannot bypass account limits. Async media concurrency covers submission, not generation time.'
      )
    ).toBe(true)
  }
)

it('updates an open account editor and its budget validation when switching languages', async () => {
  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    resources,
    lng: 'zhCN',
    fallbackLng: false,
    interpolation: { escapeValue: false },
  })
  const onSave = vi.fn()
  const account = {
    ...newAccount(),
    id: 'nv-a',
    name: 'NV-A',
    channel_ids: [1],
    budget: {
      amount: 10,
      reserve: 20,
      unit: 'usd' as const,
      period: 'day' as const,
      timezone: 'Asia/Singapore',
    },
  }
  render(
    <I18nextProvider i18n={i18n}>
      <AccountDialog
        account={account}
        channels={[
          { id: 1, name: 'NV-A', status: 1, models: 'test-model', tag: 'Mix' },
        ]}
        existing
        onClose={vi.fn()}
        onSave={onSave}
      />
    </I18nextProvider>
  )
  expect(screen.getByRole('dialog', { name: '上游账户' })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '应用' }))
  expect(await screen.findByText('预留额不能超过预算')).toBeInTheDocument()
  expect(onSave).not.toHaveBeenCalled()
  fireEvent.change(
    screen.getByRole('spinbutton', { name: i18n.t('Budget reserve') }),
    { target: { value: '0' } }
  )
  fireEvent.change(
    screen.getByRole('spinbutton', { name: i18n.t('Requests per minute') }),
    { target: { value: '-1' } }
  )
  await userEvent.click(screen.getByRole('button', { name: '应用' }))
  expect(
    await screen.findByText('请输入允许范围内的有效值')
  ).toBeInTheDocument()
  await act(() => i18n.changeLanguage('ja'))
  expect(
    screen.getByRole('dialog', { name: '上流アカウント' })
  ).toBeInTheDocument()
  expect(
    screen.getByText('許可された範囲内の有効な値を入力してください')
  ).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '適用' })).toBeInTheDocument()
})
