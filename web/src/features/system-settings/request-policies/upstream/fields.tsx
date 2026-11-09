import type {
  FieldPathByValue,
  FieldValues,
  UseFormReturn,
} from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { SettingsSwitchField } from '../../components/settings-form-layout'

type NumberProps<T extends FieldValues> = {
  form: UseFormReturn<T>
  name: FieldPathByValue<T, number>
  label: string
  max?: number
  min?: number
}
export function RoutingNumber<T extends FieldValues>(props: NumberProps<T>) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(props.label)}</FormLabel>
          <FormControl>
            <Input
              {...field}
              type='number'
              min={props.min ?? 0}
              max={props.max}
              step='any'
              value={Number(field.value)}
              onChange={(e) => field.onChange(e.target.valueAsNumber)}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
export function RoutingText<T extends FieldValues>(props: {
  form: UseFormReturn<T>
  name: FieldPathByValue<T, string>
  label: string
  disabled?: boolean
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(props.label)}</FormLabel>
          <FormControl>
            <Input
              {...field}
              value={String(field.value)}
              disabled={props.disabled}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
export function RoutingSwitch<T extends FieldValues>(props: {
  form: UseFormReturn<T>
  name: FieldPathByValue<T, boolean>
  label: string
  description?: string
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <SettingsSwitchField
            controlId={`upstream-${props.name}`}
            checked={Boolean(field.value)}
            onCheckedChange={field.onChange}
            label={t(props.label)}
            description={props.description ? t(props.description) : undefined}
          />
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
