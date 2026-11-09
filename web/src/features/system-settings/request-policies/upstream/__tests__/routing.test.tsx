import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { UpstreamRoutingSection } from '..'
import { SettingsPageProvider } from '../../../components/settings-page-context'
import { newAccount, newSource, stageDirectory, type RoutingData } from '../api'

const fixture = (): RoutingData => ({
  config: {
    revision: 1,
    enabled: false,
    max_attempts: 3,
    failure_threshold: 3,
    cooldown_seconds: 30,
    accounts: [
      {
        ...newAccount(),
        id: 'nv-a',
        name: 'NV-A',
        channel_ids: [1],
        limits: { rpm: 30, tpm: 0, concurrency: 2 },
      },
    ],
    models: [
      {
        model: 'test-model',
        enabled: true,
        sources: [{ ...newSource(1), enabled: true, verified: true }],
      },
    ],
  },
  states: {},
  channels: [
    {
      id: 1,
      name: 'NV-A',
      status: 1,
      models: 'test-model,new-model,gpt-6-sol',
      tag: 'Mix',
    },
  ],
  source_scopes: {},
})
let client: QueryClient
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: fixture() },
  })
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
})
function mount() {
  const actions = document.createElement('div')
  document.body.appendChild(actions)
  render(
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actions}>
        <UpstreamRoutingSection />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  return actions
}
it('directory import keeps existing verification, stages new models as disabled, and excludes GPT', () => {
  const data = fixture()
  const next = stageDirectory(data.config, data.channels)
  expect(
    next.models.find((p) => p.model === 'test-model')?.sources[0].verified
  ).toBe(true)
  expect(next.models.find((p) => p.model === 'new-model')).toMatchObject({
    enabled: false,
    sources: [{ enabled: false, verified: false }],
  })
  expect(next.models.some((p) => p.model.startsWith('gpt-'))).toBe(false)
  expect(data.config.models).toHaveLength(1)
})
it('loads unknown balances without inventing zero and saves through the page actions', async () => {
  const put = vi.spyOn(api, 'put').mockImplementation(async (_url, body) => ({
    data: { success: true, data: { ...(body as object), revision: 2 } },
  }))
  mount()
  await screen.findByText('NV-A', { selector: 'div' })
  expect(screen.getAllByText(/Unknown/).length).toBeGreaterThan(0)
  fireEvent.click(
    screen.getByRole('switch', { name: 'Enable upstream load balancing' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/channel/upstream_routing',
      expect.objectContaining({ enabled: true, revision: 1 })
    )
  )
})
it('account editor reuses a labeled dialog and rejects negative limits before applying', async () => {
  mount()
  await screen.findByRole('button', { name: 'Edit' })
  await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
  const dialog = screen.getByRole('dialog', { name: 'Upstream account' })
  fireEvent.change(
    within(dialog).getByRole('spinbutton', { name: 'Requests per minute' }),
    { target: { value: '-1' } }
  )
  await userEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))
  await waitFor(() =>
    expect(
      within(dialog).getByRole('spinbutton', { name: 'Requests per minute' })
    ).toHaveAttribute('aria-invalid', 'true')
  )
  expect(screen.getByRole('dialog')).toBeTruthy()
})
it('shows a retry action on an asynchronous load failure', async () => {
  vi.mocked(api.get).mockRejectedValue(new Error('offline'))
  mount()
  expect(await screen.findByText('Failed to load settings')).toBeTruthy()
  expect(screen.getByRole('button', { name: /Retry/ })).toBeTruthy()
})
