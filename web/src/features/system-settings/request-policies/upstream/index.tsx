import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { SettingsCard } from '../../components/settings-card'
import {
  SettingsFormGrid,
  SettingsSwitchField,
} from '../../components/settings-form-layout'
import { SettingsPageFormActions } from '../../components/settings-page-context'
import { AccountDialog } from './account-dialog'
import {
  configSchema,
  getRouting,
  isGPT,
  newAccount,
  newSource,
  operateRouting,
  routingQueryKey,
  saveRouting,
  stageDirectory,
  type ModelPool,
  type RoutingConfig,
  type RoutingData,
  type UpstreamAccount,
  type UpstreamSource,
} from './api'
import { RoutingNumber, RoutingSwitch } from './fields'
import { RuntimeStatus } from './runtime-status'
import { SourceDialog } from './source-dialog'

export function UpstreamRoutingSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: routingQueryKey,
    queryFn: getRouting,
    refetchInterval: 5000,
  })
  if (query.isPending) return <LoadingState />
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load settings')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return <UpstreamEditor data={query.data} />
}
function UpstreamEditor(props: { data: RoutingData }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const form = useForm<RoutingConfig>({
    resolver: zodResolver(configSchema),
    defaultValues: props.data.config,
  })
  const config = useWatch({ control: form.control }) as RoutingConfig
  const [account, setAccount] = useState<{
    value: UpstreamAccount
    index: number
  } | null>(null)
  const [source, setSource] = useState<{
    value: UpstreamSource
    index: number
  } | null>(null)
  const [modelName, setModelName] = useState('')
  const [search, setSearch] = useState('')
  const [newModel, setNewModel] = useState<string | null>(null)
  const [operation, setOperation] = useState<{
    scope: string
    action: 'recover' | 'reset_budget'
  } | null>(null)
  const [removeSource, setRemoveSource] = useState<number | null>(null)
  const save = useMutation({
    mutationFn: saveRouting,
    onSuccess: async (value) => {
      form.reset(value)
      await client.invalidateQueries({ queryKey: routingQueryKey })
      toast.success(t('Settings saved'))
    },
  })
  const operate = useMutation({
    mutationFn: (value: {
      scope: string
      action: 'recover' | 'reset_budget'
    }) => operateRouting(value.scope, value.action),
    onSuccess: async () => {
      setOperation(null)
      await client.invalidateQueries({ queryKey: routingQueryKey })
    },
  })
  const update = (next: RoutingConfig) => {
    form.setValue('accounts', next.accounts, { shouldDirty: true })
    form.setValue('models', next.models, { shouldDirty: true })
  }
  const submit = form.handleSubmit(
    async (value) => {
      try {
        await save.mutateAsync(value)
      } catch (e) {
        handleServerError(e)
      }
    },
    () => toast.error(t('Invalid upstream routing configuration'))
  )
  const poolIndex = config.models.findIndex((p) => p.model === modelName)
  const pool = config.models[poolIndex]
  const accountChannels = new Set(config.accounts.flatMap((a) => a.channel_ids))
  const sourceChannels = props.data.channels.filter(
    (c) => accountChannels.has(c.id) && c.tag !== 'GPT'
  )
  const changePool = (next: ModelPool) =>
    form.setValue(`models.${poolIndex}`, next, { shouldDirty: true })
  const canSave =
    form.formState.isDirty && props.data.config.revision === config.revision
  return (
    <div className='space-y-6'>
      <SettingsPageFormActions
        onSave={() => void submit()}
        onReset={() => form.reset(props.data.config)}
        isSaving={save.isPending}
        isSaveDisabled={!canSave}
      />
      {props.data.config.revision !== config.revision && (
        <p role='alert' className='text-destructive'>
          {t(
            'Configuration changed elsewhere. Reload settings before editing.'
          )}
        </p>
      )}
      <SettingsCard
        title={t('Upstream load balancing')}
        description={t(
          'GPT routing is excluded. Saving applies to new requests; running requests finish with their original policy.'
        )}
      >
        <Form {...form}>
          <form onSubmit={submit}>
            <SettingsFormGrid>
              <RoutingSwitch
                form={form}
                name='enabled'
                label='Enable upstream load balancing'
                description='Turning this off restores the original channel scheduler.'
              />
              <RoutingNumber
                form={form}
                name='max_attempts'
                label='Maximum total attempts'
                min={1}
                max={3}
              />
              <RoutingNumber
                form={form}
                name='failure_threshold'
                label='Failures before circuit opens'
                min={1}
                max={20}
              />
              <RoutingNumber
                form={form}
                name='cooldown_seconds'
                label='Initial failure cooldown (seconds)'
                min={1}
                max={300}
              />
            </SettingsFormGrid>
          </form>
        </Form>
      </SettingsCard>
      <SettingsCard
        title={t('Upstream accounts')}
        description={t(
          'Consumption persists across restarts. Recovery does not reset consumption or enable manually disabled channels.'
        )}
      >
        <div className='mb-4 flex flex-wrap gap-2'>
          <Button
            variant='outline'
            onClick={() => setAccount({ value: newAccount(), index: -1 })}
          >
            {t('Add account')}
          </Button>
        </div>
        <StaticDataTable
          data={config.accounts}
          getRowKey={(a) => a.id}
          emptyContent={t('No upstream accounts configured')}
          columns={[
            {
              id: 'name',
              header: t('Account'),
              cell: (a) => (
                <div>
                  {a.name}
                  <p className='text-muted-foreground text-xs'>
                    {a.id} ·{' '}
                    {a.channel_ids
                      .map(
                        (id) =>
                          props.data.channels.find((c) => c.id === id)?.name ??
                          `#${id}`
                      )
                      .join(', ')}
                  </p>
                </div>
              ),
            },
            {
              id: 'limits',
              header: t('Limits'),
              cell: (a) => (
                <span>
                  {formatNumber(a.limits.rpm, locale)} RPM /{' '}
                  {formatNumber(a.limits.tpm, locale)} TPM /{' '}
                  {formatNumber(a.limits.concurrency, locale)}
                </span>
              ),
            },
            {
              id: 'state',
              header: t('Runtime status'),
              cell: (a) => (
                <RuntimeStatus
                  state={props.data.states[`account:${a.id}`]}
                  paused={a.paused}
                  account={a}
                />
              ),
            },
            {
              id: 'actions',
              header: t('Actions'),
              cell: (a, index) => (
                <div className='flex flex-wrap gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => setAccount({ value: a, index })}
                  >
                    {t('Edit')}
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={!props.data.states[`account:${a.id}`]}
                    onClick={() =>
                      setOperation({
                        scope: `account:${a.id}`,
                        action: 'recover',
                      })
                    }
                  >
                    {t('Recover')}
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={!props.data.states[`account:${a.id}`]}
                    onClick={() =>
                      setOperation({
                        scope: `account:${a.id}`,
                        action: 'reset_budget',
                      })
                    }
                  >
                    {t('Reset consumption')}
                  </Button>
                </div>
              ),
            },
          ]}
        />
      </SettingsCard>
      <SettingsCard
        title={t('Model source pools')}
        description={t(
          'New directory entries remain pending until verified and enabled. Aliases must represent equivalent models.'
        )}
      >
        <div className='mb-4 flex flex-wrap gap-2'>
          <Input
            aria-label={t('Search models')}
            placeholder={t('Search models')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <NativeSelect
            aria-label={t('Model')}
            value={modelName}
            onChange={(e) => setModelName(e.target.value)}
          >
            <NativeSelectOption value=''>
              {t('Select model')}
            </NativeSelectOption>
            {config.models
              .filter((p) =>
                p.model.toLowerCase().includes(search.toLowerCase())
              )
              .map((p) => (
                <NativeSelectOption key={p.model} value={p.model}>
                  {p.model} (
                  {p.sources.filter((s) => s.enabled && s.verified).length}/
                  {p.sources.length})
                </NativeSelectOption>
              ))}
          </NativeSelect>
          <Button variant='outline' onClick={() => setNewModel('')}>
            {t('Add model pool')}
          </Button>
          <Button
            variant='outline'
            onClick={() => {
              update(stageDirectory(config, props.data.channels))
              toast.info(
                t('Directory entries staged as pending. Save to apply.')
              )
            }}
          >
            {t('Import channel directory')}
          </Button>
        </div>
        {pool && (
          <div className='space-y-4'>
            <SettingsSwitchField
              controlId='upstream-model-enabled'
              checked={pool.enabled}
              onCheckedChange={(enabled) => changePool({ ...pool, enabled })}
              label={pool.model}
            />
            <Button
              variant='outline'
              disabled={sourceChannels.length === 0}
              onClick={() =>
                setSource({
                  value: newSource(sourceChannels[0]?.id ?? 0),
                  index: -1,
                })
              }
            >
              {t('Add model source')}
            </Button>
            <StaticDataTable
              data={pool.sources}
              getRowKey={(s) => s.channel_id}
              emptyContent={t('No model sources configured')}
              columns={[
                {
                  id: 'source',
                  header: t('Channel'),
                  cell: (s) => (
                    <div>
                      {props.data.channels.find((c) => c.id === s.channel_id)
                        ?.name ?? `#${s.channel_id}`}
                      <p className='text-muted-foreground text-xs'>
                        {s.model || pool.model}
                      </p>
                      {props.data.channels.find((c) => c.id === s.channel_id)
                        ?.status !== 1 && (
                        <Badge variant='secondary'>
                          {t('Channel disabled')}
                        </Badge>
                      )}
                    </div>
                  ),
                },
                {
                  id: 'verified',
                  header: t('Traffic'),
                  cell: (s) => (
                    <Badge
                      variant={
                        s.verified && s.enabled ? 'default' : 'secondary'
                      }
                    >
                      {s.verified && s.enabled
                        ? t('Enabled')
                        : t('Pending or disabled')}
                    </Badge>
                  ),
                },
                {
                  id: 'weight',
                  header: t('Priority / weight / cost tier'),
                  cell: (s) =>
                    `${s.priority} / ${s.weight} / ${s.cost_known ? s.cost_tier : t('Unknown')}`,
                },
                {
                  id: 'state',
                  header: t('Runtime status'),
                  cell: (s) => (
                    <RuntimeStatus
                      state={
                        props.data.states[
                          props.data.source_scopes?.[pool.model]?.[
                            s.channel_id
                          ] ?? ''
                        ]
                      }
                    />
                  ),
                },
                {
                  id: 'actions',
                  header: t('Actions'),
                  cell: (s, index) => (
                    <div className='flex gap-2'>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => setSource({ value: s, index })}
                      >
                        {t('Edit')}
                      </Button>
                      <Button
                        variant='outline'
                        size='sm'
                        disabled={
                          !props.data.source_scopes?.[pool.model]?.[
                            s.channel_id
                          ]
                        }
                        onClick={() =>
                          setOperation({
                            scope:
                              props.data.source_scopes[pool.model][
                                s.channel_id
                              ],
                            action: 'recover',
                          })
                        }
                      >
                        {t('Recover')}
                      </Button>
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => setRemoveSource(index)}
                      >
                        {t('Remove')}
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          </div>
        )}
      </SettingsCard>
      {account && (
        <AccountDialog
          account={account.value}
          existing={account.index >= 0}
          channels={props.data.channels}
          onClose={() => setAccount(null)}
          onSave={(value) => {
            const accounts = [...config.accounts]
            if (account.index < 0) accounts.push(value)
            else accounts[account.index] = value
            form.setValue('accounts', accounts, { shouldDirty: true })
            setAccount(null)
          }}
        />
      )}
      {source && pool && (
        <SourceDialog
          source={source.value}
          channels={sourceChannels}
          onClose={() => setSource(null)}
          onSave={(value) => {
            const sources = [...pool.sources]
            if (source.index < 0) sources.push(value)
            else sources[source.index] = value
            changePool({ ...pool, sources })
            setSource(null)
          }}
        />
      )}
      {newModel !== null && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) setNewModel(null)
          }}
          title={t('Add model pool')}
          footer={
            <Button
              disabled={
                !newModel.trim() ||
                isGPT(newModel) ||
                config.models.some((p) => p.model === newModel.trim())
              }
              onClick={() => {
                const name = newModel.trim()
                form.setValue(
                  'models',
                  [
                    ...config.models,
                    { model: name, enabled: false, sources: [] },
                  ],
                  { shouldDirty: true }
                )
                setModelName(name)
                setNewModel(null)
              }}
            >
              {t('Add')}
            </Button>
          }
        >
          <Label htmlFor='new-pool-name'>{t('Model')}</Label>
          <Input
            id='new-pool-name'
            value={newModel}
            onChange={(e) => setNewModel(e.target.value)}
          />
        </Dialog>
      )}
      <ConfirmDialog
        open={operation !== null}
        onOpenChange={(open) => {
          if (!open) setOperation(null)
        }}
        title={
          operation?.action === 'reset_budget'
            ? t('Reset consumption')
            : t('Recover upstream source')
        }
        desc={
          operation?.action === 'reset_budget'
            ? t(
                'This clears local budget and balance consumption. Active requests must finish first. Upstream provider credits are unchanged.'
              )
            : t(
                'Clear system pause and cooldown, then allow one recovery probe. Consumption and channel switches remain unchanged.'
              )
        }
        isLoading={operate.isPending}
        handleConfirm={() => {
          if (operation) {
            void operate.mutateAsync(operation).catch(handleServerError)
          }
        }}
      />
      <ConfirmDialog
        open={removeSource !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveSource(null)
        }}
        title={t('Remove model source')}
        desc={t(
          'Remove this source from the model pool? Save settings to apply.'
        )}
        handleConfirm={() => {
          if (pool && removeSource !== null) {
            changePool({
              ...pool,
              sources: pool.sources.filter(
                (_, index) => index !== removeSource
              ),
            })
          }
          setRemoveSource(null)
        }}
      />
    </div>
  )
}
