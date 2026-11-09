import type { TFunction } from 'i18next'

// Labels for the decision events recorded by the backend request policy state
// (reasons, sources and health), the session behaviors, and the auto-disable
// sources reported by the channel operations summary.
export function policyLabel(t: TFunction, value: string): string {
  switch (value) {
    case 'rpm_limit':
      return t('Requests per minute limit reached')
    case 'tpm_limit':
      return t('Tokens per minute limit reached')
    case 'concurrency_limit':
      return t('Concurrent request limit reached')
    case 'half_open_probe':
      return t('Recovery probe already running')
    case 'cooldown':
      return t('Cooling down')
    case 'account_paused':
      return t('Account paused')
    case 'budget_reserve':
      return t('Budget reserve reached')
    case 'upstream_balance':
      return t('Upstream balance exhausted')
    case 'invalid_credentials':
      return t('Invalid upstream credentials')
    case 'upstream_quota_exhausted':
      return t('Upstream quota exhausted')
    case 'model_unavailable':
      return t('Model unavailable on this source')
    case 'rate_limit':
      return t('Upstream rate limit')
    case 'temporary_failure':
      return t('Temporary upstream failure')
    case 'request_rejected':
      return t('Request rejected by upstream')
    case 'upstream_capacity_reserved':
      return t('Upstream capacity reserved')
    case 'upstream_capacity':
      return t('Upstream capacity')
    case 'upstream_routing':
      return t('Upstream load balancing')
    case 'ambiguous_media_submission':
      return t('Submission outcome is uncertain; automatic retry stopped')
    case 'response_already_started':
      return t('Response already started')

    case 'upstream_failure':
      return t('Upstream request failed')
    case 'request_failed':
      return t('Request failed')
    case 'task_failed':
      return t('Task failed')
    case 'strict_session':
      return t('Session behavior prevents switching channels')
    case 'attempt_budget_exhausted':
      return t('Total attempt budget exhausted')
    case 'channel_error':
      return t('Channel error')
    case 'unrecognized_status':
      return t('Unrecognized status code')
    case 'task_accepted':
      return t('The task was accepted; submission cannot be replayed')
    case 'pinned_channel':
      return t('Fixed channel constraint')
    case 'local_rejection':
      return t('Request rejected locally')
    case 'non_retryable_error':
    case 'system_retry_exclusion':
      return t('System retry exclusion')
    case 'status_not_retryable':
      return t('Status is outside the retry rules')
    case 'retry_status_matched':
      return t('Status matches the retry rules')
    case 'session_rule_matched':
      return t('Session rule matched')
    case 'channel_selected':
      return t('Channel selected')
    case 'request_completed':
      return t('Request completed')
    case 'stream_not_successful':
      return t('Stream did not complete successfully')
    case 'channel':
      return t('Channel')
    case 'global':
      return t('Global default')
    case 'global_and_channel':
      return t('Global and channel settings')
    case 'session_rule':
      return t('Session rule')
    case 'system':
      return t('System constraint')
    case 'routing':
      return t('Channel selection')
    case 'upstream':
      return t('Upstream response')
    case 'transport':
      return t('Upstream connection')
    case 'local':
      return t('Local check')
    case 'channel_constraint':
      return t('Fixed channel constraint')
    case 'unchanged':
      return t('Unchanged')
    case 'channel_disable_requested':
      return t('Channel disable requested')
    case 'key_disable_requested':
      return t('Current key disable requested')
    case 'off':
    case '':
      return t('Do not keep sessions')
    case 'prefer':
      return t('Prefer the original channel, allow switching')
    case 'strict':
      return t('Require the original channel')
    default:
      return value
  }
}
