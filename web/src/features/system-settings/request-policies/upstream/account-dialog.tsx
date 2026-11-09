import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { SettingsFormGrid } from '../../components/settings-form-layout'
import { accountSchema, type Channel, type UpstreamAccount } from './api'
import { RoutingNumber, RoutingSwitch, RoutingText } from './fields'

export function AccountDialog(props: {
  account: UpstreamAccount
  channels: Channel[]
  existing: boolean
  onClose: () => void
  onSave: (account: UpstreamAccount) => void
}) {
  const { t } = useTranslation()
  const form = useForm<UpstreamAccount>({
    resolver: zodResolver(accountSchema),
    defaultValues: props.account,
  })
  const submit = form.handleSubmit((value) => {
    if (value.balance !== props.account.balance) {
      value.balance_source = value.balance === null ? '' : 'manual'
      value.balance_updated_at = Math.floor(Date.now() / 1000)
    }
    props.onSave(value)
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Upstream account')}
      description={t(
        'Limits are shared by every channel in this account. Zero means no configured limit.'
      )}
      footer={<Button onClick={() => void submit()}>{t('Apply')}</Button>}
    >
      <Form {...form}>
        <form onSubmit={submit} className='space-y-4'>
          <SettingsFormGrid>
            <RoutingText
              form={form}
              name='id'
              label='Account ID'
              disabled={props.existing}
            />
            <RoutingText form={form} name='name' label='Name' />
            <RoutingSwitch form={form} name='paused' label='Pause account' />
            <FormField
              control={form.control}
              name='channel_ids'
              render={({ field }) => (
                <FormItem>
                  <FormLabel htmlFor='upstream-account-channels'>
                    {t('Channels')}
                  </FormLabel>
                  <MultiSelect
                    id='upstream-account-channels'
                    options={props.channels
                      .filter((c) => c.tag !== 'GPT')
                      .map((c) => ({
                        value: String(c.id),
                        label: `${c.name} (#${c.id})`,
                      }))}
                    selected={field.value.map(String)}
                    onChange={(v) => field.onChange(v.map(Number))}
                  />
                  <FormMessage />
                </FormItem>
              )}
            />
            <RoutingNumber
              form={form}
              name='limits.rpm'
              label='Requests per minute'
              max={100000}
            />
            <RoutingNumber
              form={form}
              name='limits.tpm'
              label='Tokens per minute'
              max={1e12}
            />
            <RoutingNumber
              form={form}
              name='limits.concurrency'
              label='Concurrent upstream requests'
              max={100000}
            />
            <RoutingNumber
              form={form}
              name='budget.amount'
              label='Upstream budget'
              max={1e12}
            />
            <RoutingNumber
              form={form}
              name='budget.reserve'
              label='Budget reserve'
              max={1e12}
            />
            <FormField
              control={form.control}
              name='budget.unit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Budget unit')}</FormLabel>
                  <FormControl>
                    <NativeSelect {...field}>
                      <NativeSelectOption value=''>
                        {t('Unknown')}
                      </NativeSelectOption>
                      <NativeSelectOption value='requests'>
                        {t('Requests')}
                      </NativeSelectOption>
                      <NativeSelectOption value='tokens'>
                        {t('Tokens')}
                      </NativeSelectOption>
                      <NativeSelectOption value='usd'>USD</NativeSelectOption>
                    </NativeSelect>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='budget.period'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Reset period')}</FormLabel>
                  <FormControl>
                    <NativeSelect {...field}>
                      <NativeSelectOption value=''>
                        {t('Unknown')}
                      </NativeSelectOption>
                      <NativeSelectOption value='day'>
                        {t('Daily')}
                      </NativeSelectOption>
                      <NativeSelectOption value='month'>
                        {t('Monthly')}
                      </NativeSelectOption>
                      <NativeSelectOption value='lifetime'>
                        {t('Lifetime')}
                      </NativeSelectOption>
                    </NativeSelect>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <RoutingText
              form={form}
              name='budget.timezone'
              label='Reset timezone'
            />
            <FormField
              control={form.control}
              name='balance'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Manual upstream balance (USD)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step='any'
                      placeholder={t('Unknown')}
                      value={field.value ?? ''}
                      onChange={(e) =>
                        field.onChange(
                          e.target.value === '' ? null : e.target.valueAsNumber
                        )
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsFormGrid>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Budget and balance are upstream estimates. Gateway user billing is separate. Leave unknown values unset.'
            )}
          </p>
        </form>
      </Form>
    </Dialog>
  )
}
