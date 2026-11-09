import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { SettingsFormGrid } from '../../components/settings-form-layout'
import { sourceSchema, type Channel, type UpstreamSource } from './api'
import { RoutingNumber, RoutingSwitch, RoutingText } from './fields'

export function SourceDialog(props: {
  source: UpstreamSource
  channels: Channel[]
  onClose: () => void
  onSave: (source: UpstreamSource) => void
}) {
  const { t } = useTranslation()
  const form = useForm<UpstreamSource>({
    resolver: zodResolver(sourceSchema),
    defaultValues: props.source,
  })
  const submit = form.handleSubmit(props.onSave)
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Model source')}
      description={t(
        'Higher priority wins. Within a priority, known lower cost tiers win; weight balances equivalent sources.'
      )}
      footer={<Button onClick={() => void submit()}>{t('Apply')}</Button>}
    >
      <Form {...form}>
        <form onSubmit={submit} className='space-y-4'>
          <SettingsFormGrid>
            <FormField
              control={form.control}
              name='channel_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel')}</FormLabel>
                  <FormControl>
                    <NativeSelect
                      value={field.value}
                      onChange={(e) => field.onChange(Number(e.target.value))}
                    >
                      {props.channels.map((c) => (
                        <NativeSelectOption key={c.id} value={c.id}>
                          {c.name} (#{c.id})
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <RoutingText
              form={form}
              name='model'
              label='Source model alias (optional)'
            />
            <RoutingSwitch form={form} name='enabled' label='Enable source' />
            <RoutingSwitch
              form={form}
              name='verified'
              label='Verified source'
              description='Only mark verified after a successful probe. Directory refresh never verifies a source.'
            />
            <RoutingNumber
              form={form}
              name='priority'
              label='Priority'
              max={100000}
            />
            <RoutingNumber
              form={form}
              name='weight'
              label='Weight'
              min={1}
              max={100000}
            />
            <RoutingSwitch
              form={form}
              name='cost_known'
              label='Upstream cost is known'
              description='Unknown cost is never assumed to be free.'
            />
            <RoutingNumber
              form={form}
              name='cost_tier'
              label='Cost tier'
              max={1000}
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
              name='input_usd_per_million'
              label='Input USD per million tokens'
              max={1e6}
            />
            <RoutingNumber
              form={form}
              name='output_usd_per_million'
              label='Output USD per million tokens'
              max={1e6}
            />
            <RoutingNumber
              form={form}
              name='request_usd'
              label='USD per request'
              max={1e6}
            />
          </SettingsFormGrid>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Source limits cannot bypass account limits. Async media concurrency covers submission, not generation time.'
            )}
          </p>
        </form>
      </Form>
    </Dialog>
  )
}
