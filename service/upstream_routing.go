package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const upstreamRequestKey = "upstream_routing_request"
const UpstreamRetryAfterKey = "upstream_retry_after"

// UpstreamUsage is an upstream capacity estimate, entirely independent of the
// user's gateway wallet. Unknown usage retains the conservative reservation.
type UpstreamUsage struct {
	Requests float64 `json:"requests"`
	Tokens   float64 `json:"tokens"`
	USD      float64 `json:"usd"`
}
type UpstreamRateEvent struct {
	ID     string `json:"id"`
	At     int64  `json:"at"`
	Tokens int64  `json:"tokens"`
}
type UpstreamLease struct {
	Expires int64 `json:"expires"`
}
type UpstreamRuntime struct {
	BalanceEpoch      int64                    `json:"balance_epoch"`
	BalanceUsed       float64                  `json:"balance_used"`
	EstimatedRequests int64                    `json:"estimated_requests"`
	Period            string                   `json:"period"`
	Usage             UpstreamUsage            `json:"usage"`
	Lifetime          UpstreamUsage            `json:"lifetime"`
	Events            []UpstreamRateEvent      `json:"events"`
	Leases            map[string]UpstreamLease `json:"leases"`
	Paused            string                   `json:"paused"`
	CooldownUntil     int64                    `json:"cooldown_until"`
	Failures          int                      `json:"failures"`
	Backoff           int                      `json:"backoff"`
	Recovering        bool                     `json:"recovering"`
	RecoverySuccesses int                      `json:"recovery_successes"`
	Successes         int64                    `json:"successes"`
	Errors            int64                    `json:"errors"`
	Estimated         bool                     `json:"estimated"`
}
type upstreamAttempt struct {
	ID            string
	Sent          bool
	Account       dto.UpstreamAccount
	Source        dto.UpstreamSource
	Scopes        []string
	Estimate      UpstreamUsage
	Input, Output int64
	Period        string
	Stop          chan struct{}
	Once          sync.Once
}
type upstreamRequest struct {
	Config   dto.UpstreamRoutingConfig
	Model    string
	Managed  bool
	Attempts int
	Tried    map[int]bool
	Current  *upstreamAttempt
}

func upstreamPolicy(c *gin.Context, name string) (*upstreamRequest, error) {
	if v, ok := c.Get(upstreamRequestKey); ok {
		return v.(*upstreamRequest), nil
	}
	p := &upstreamRequest{Model: name, Tried: map[int]bool{}}
	if model.DB == nil || model.IsUnmanagedGPTModel(name) {
		c.Set(upstreamRequestKey, p)
		return p, nil
	}
	cfg, err := model.ReadUpstreamRoutingConfig()
	if err != nil {
		return nil, errors.New(i18n.T(c, i18n.MsgUpstreamRoutingStateUnavailable))
	}
	p.Config, p.Managed = cfg, cfg.Enabled
	c.Set(upstreamRequestKey, p)
	return p, nil
}
func IsUpstreamManaged(c *gin.Context) bool {
	v, ok := c.Get(upstreamRequestKey)
	return ok && v.(*upstreamRequest).Managed
}
func UpstreamSourceScope(channel int, name string) string {
	return fmt.Sprintf("source:%d:%x", channel, sha256.Sum256([]byte(name)))
}
func UpstreamSourceModel(modelName string, s dto.UpstreamSource) string {
	if s.Model != "" {
		return s.Model
	}
	return modelName
}

func budgetPeriod(b dto.UpstreamBudget, now time.Time) string {
	loc := time.UTC
	if b.Timezone != "" {
		if l, err := time.LoadLocation(b.Timezone); err == nil {
			loc = l
		}
	}
	switch b.Period {
	case "day":
		return now.In(loc).Format("2006-01-02") + "@" + loc.String()
	case "month":
		return now.In(loc).Format("2006-01") + "@" + loc.String()
	default:
		return "lifetime"
	}
}
func (u UpstreamUsage) value(unit string) float64 {
	switch unit {
	case "requests":
		return u.Requests
	case "tokens":
		return u.Tokens
	case "usd":
		return u.USD
	}
	return 0
}
func (u *UpstreamUsage) add(v UpstreamUsage) {
	u.Requests = max(0, u.Requests+v.Requests)
	u.Tokens = max(0, u.Tokens+v.Tokens)
	u.USD = max(0, u.USD+v.USD)
}
func decodeUpstreamState(row *model.UpstreamRoutingRecord, now time.Time) (UpstreamRuntime, error) {
	var s UpstreamRuntime
	if err := common.UnmarshalJsonStr(string(row.Document), &s); err != nil {
		return s, err
	}
	if s.Leases == nil {
		s.Leases = map[string]UpstreamLease{}
	}
	for id, l := range s.Leases {
		if l.Expires <= now.Unix() {
			delete(s.Leases, id)
		}
	}
	s.Events = slices.DeleteFunc(s.Events, func(e UpstreamRateEvent) bool { return e.At <= now.Unix()-60 })
	return s, nil
}
func encodeUpstreamState(row *model.UpstreamRoutingRecord, s UpstreamRuntime) error {
	b, err := common.Marshal(s)
	row.Document = model.UpstreamRoutingDocument(b)
	return err
}
func upstreamCapacity(s UpstreamRuntime, l dto.UpstreamLimits, tokens int64, now time.Time) string {
	if s.Paused != "" {
		return s.Paused
	}
	if s.CooldownUntil > now.Unix() {
		return "cooldown"
	}
	if s.Recovering && len(s.Leases) > 0 {
		return "half_open_probe"
	}
	if l.Concurrency > 0 && len(s.Leases) >= l.Concurrency {
		return "concurrency_limit"
	}
	if l.RPM > 0 && len(s.Events) >= l.RPM {
		return "rpm_limit"
	}
	var used int64
	for _, e := range s.Events {
		used += e.Tokens
	}
	if l.TPM > 0 && tokens > l.TPM-used {
		return "tpm_limit"
	}
	return ""
}
func estimateUpstreamRequest(c *gin.Context, s dto.UpstreamSource) (int64, int64, UpstreamUsage, error) {
	body, err := common.GetBodyStorage(c)
	if err != nil {
		return 0, 0, UpstreamUsage{}, err
	}
	// Bytes are a conservative text-token proxy, not an upstream balance. The
	// estimate is visible and replaced only when the provider reports usage.
	input := min(body.Size(), int64(1e9))
	output := int64(32768)
	if body.Size() <= 8<<20 && strings.Contains(c.GetHeader("Content-Type"), "json") {
		data, e := body.Bytes()
		if e != nil {
			return 0, 0, UpstreamUsage{}, e
		}
		for _, path := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "generationConfig.maxOutputTokens"} {
			if v := gjson.GetBytes(data, path); v.Exists() && v.Int() > 0 {
				output = min(v.Int(), int64(1e9))
				break
			}
		}
	}
	return input, output, UpstreamUsage{Requests: 1, Tokens: float64(input + output), USD: s.RequestUSD + (float64(input)*s.InputUSDPerMillion+float64(output)*s.OutputUSDPerMillion)/1e6}, nil
}
func reserveUpstream(c *gin.Context, p *upstreamRequest, a dto.UpstreamAccount, s dto.UpstreamSource, now time.Time) error {
	identity := UpstreamSourceModel(p.Model, s)
	// Aliases of the same exposed source share cooldown, reservations and the
	// strictest configured model limit, so another client name cannot evade it.
	for _, pool := range p.Config.Models {
		for _, other := range pool.Sources {
			if other.ChannelID != s.ChannelID || UpstreamSourceModel(pool.Model, other) != identity {
				continue
			}
			if other.Limits.RPM > 0 && (s.Limits.RPM == 0 || other.Limits.RPM < s.Limits.RPM) {
				s.Limits.RPM = other.Limits.RPM
			}
			if other.Limits.TPM > 0 && (s.Limits.TPM == 0 || other.Limits.TPM < s.Limits.TPM) {
				s.Limits.TPM = other.Limits.TPM
			}
			if other.Limits.Concurrency > 0 && (s.Limits.Concurrency == 0 || other.Limits.Concurrency < s.Limits.Concurrency) {
				s.Limits.Concurrency = other.Limits.Concurrency
			}
		}
	}
	input, output, estimate, err := estimateUpstreamRequest(c, s)
	if err != nil {
		return err
	}
	at := &upstreamAttempt{ID: common.GetUUID(), Account: a, Source: s, Scopes: []string{"account:" + a.ID, UpstreamSourceScope(s.ChannelID, identity)}, Estimate: estimate, Input: input, Output: output, Period: budgetPeriod(a.Budget, now), Stop: make(chan struct{})}
	err = model.UpdateUpstreamRoutingRecords(at.Scopes, func(rows map[string]*model.UpstreamRoutingRecord) error {
		for i, scope := range at.Scopes {
			state, e := decodeUpstreamState(rows[scope], now)
			if e != nil {
				return e
			}
			limits := s.Limits
			if i == 0 {
				limits = a.Limits
			}
			if why := upstreamCapacity(state, limits, input+output, now); why != "" {
				return errors.New(why)
			}
			if i == 0 {
				if a.Paused {
					return errors.New("account_paused")
				}
				if state.Period != at.Period {
					state.Period = at.Period
					state.Usage = UpstreamUsage{}
				}
				if a.Budget.Amount > 0 && state.Usage.value(a.Budget.Unit)+estimate.value(a.Budget.Unit) > a.Budget.Amount-a.Budget.Reserve {
					return errors.New("budget_reserve")
				}
				if state.BalanceEpoch != a.BalanceUpdatedAt {
					state.BalanceEpoch = a.BalanceUpdatedAt
					state.BalanceUsed = 0
				}
				if a.Balance != nil && state.BalanceUsed+estimate.USD > *a.Balance {
					return errors.New("upstream_balance")
				}
			}
			state.Usage.add(estimate)
			state.Lifetime.add(estimate)
			state.EstimatedRequests++
			state.Estimated = true
			if i == 0 {
				state.BalanceUsed += estimate.USD
			}
			state.Events = append(state.Events, UpstreamRateEvent{ID: at.ID, At: now.Unix(), Tokens: input + output})
			state.Leases[at.ID] = UpstreamLease{Expires: now.Add(120 * time.Second).Unix()}
			if e = encodeUpstreamState(rows[scope], state); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.Current = at
	p.Attempts++
	p.Tried[s.ChannelID] = true
	c.Set(UpstreamRetryAfterKey, "")
	go at.heartbeat()
	return nil
}
func (a *upstreamAttempt) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.Stop:
			return
		case now := <-ticker.C:
			err := model.UpdateUpstreamRoutingRecords(a.Scopes, func(rows map[string]*model.UpstreamRoutingRecord) error {
				for _, scope := range a.Scopes {
					s, e := decodeUpstreamState(rows[scope], now)
					if e != nil {
						return e
					}
					if _, ok := s.Leases[a.ID]; ok {
						s.Leases[a.ID] = UpstreamLease{Expires: now.Add(120 * time.Second).Unix()}
					}
					if e = encodeUpstreamState(rows[scope], s); e != nil {
						return e
					}
				}
				return nil
			})
			if err != nil {
				common.SysError("upstream lease heartbeat failed")
			}
		}
	}
}

type upstreamCandidate struct {
	channel *model.Channel
	source  dto.UpstreamSource
	account dto.UpstreamAccount
	group   string
	rank    float64
}

// SelectUpstreamChannel returns handled=true whenever this policy owns the
// request, including when all sources are unavailable. It never falls back to
// the old scheduler and thereby bypasses account limits.
func SelectUpstreamChannel(param *RetryParam, forced *model.Channel) (*model.Channel, string, bool, error) {
	c := param.Ctx
	p, err := upstreamPolicy(c, param.ModelName)
	if err != nil {
		return nil, "", true, err
	}
	if !p.Managed {
		return nil, "", false, nil
	}
	if p.Current != nil {
		return forced, "", true, errors.New(i18n.T(c, i18n.MsgUpstreamRoutingActiveAttempt))
	}
	if p.Attempts >= p.Config.MaxAttempts {
		return nil, "", true, errors.New(i18n.T(c, i18n.MsgUpstreamRoutingAttemptBudgetExhausted))
	}
	var pool *dto.UpstreamModelPool
	for i := range p.Config.Models {
		if p.Config.Models[i].Model == p.Model {
			pool = &p.Config.Models[i]
			break
		}
	}
	if pool == nil || !pool.Enabled {
		return nil, "", true, errors.New(i18n.T(c, i18n.MsgUpstreamRoutingPendingModel))
	}
	pinned := 0
	if pin, ok, _ := GetChannelConstraints(c).ResolvedPin(); ok {
		pinned = pin.ChannelId
	}
	if forced != nil {
		pinned = forced.Id
	}
	preferred, hasPreferred := GetPreferredChannelByAffinity(c, p.Model, param.TokenGroup)
	if hasPreferred && RequestPolicy(c).SessionMode == "strict" {
		pinned = preferred
	}
	groups := []string{param.TokenGroup}
	if param.TokenGroup == "auto" {
		groups = GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
	}
	var candidates []upstreamCandidate
	for _, src := range pool.Sources {
		if !src.Enabled || !src.Verified || p.Tried[src.ChannelID] || pinned != 0 && pinned != src.ChannelID {
			continue
		}
		ch, e := model.CacheGetChannel(src.ChannelID)
		if e != nil || ch == nil || ch.Status != common.ChannelStatusEnabled {
			continue
		}
		exposed := src.Model
		if exposed == "" {
			exposed = p.Model
		}
		if ok, _ := model.ChannelSatisfiesFilters(ch, exposed, GetChannelConstraints(c).Filters); !ok {
			continue
		}
		group := ""
		for _, g := range groups {
			if model.IsChannelEnabledForGroupModel(g, exposed, ch.Id) {
				group = g
				break
			}
		}
		if group == "" {
			continue
		}
		var account dto.UpstreamAccount
		for _, a := range p.Config.Accounts {
			if slices.Contains(a.ChannelIDs, ch.Id) {
				account = a
				break
			}
		}
		if account.ID == "" || account.Paused {
			continue
		}
		// Copy the cached channel before adding a canonical alias. Never mutate the
		// shared channel cache or stored provider mappings.
		clone := *ch
		if exposed != p.Model {
			mapping := map[string]string{}
			if raw := ch.GetModelMapping(); raw != "" {
				if common.UnmarshalJsonStr(raw, &mapping) != nil {
					continue
				}
			}
			raw := exposed
			for range 16 {
				next, ok := mapping[raw]
				if !ok || next == raw {
					break
				}
				raw = next
			}
			mapping[p.Model] = raw
			b, e := common.Marshal(mapping)
			if e != nil {
				continue
			}
			v := string(b)
			clone.ModelMapping = &v
		}
		candidates = append(candidates, upstreamCandidate{channel: &clone, source: src, account: account, group: group, rank: -math.Log(max(rand.Float64(), 1e-15)) / float64(max(src.Weight, 1))})
	}
	slices.SortStableFunc(candidates, func(a, b upstreamCandidate) int {
		if a.source.Priority != b.source.Priority {
			return b.source.Priority - a.source.Priority
		}
		if a.source.CostKnown != b.source.CostKnown {
			if a.source.CostKnown {
				return -1
			}
			return 1
		}
		if a.source.CostTier != b.source.CostTier {
			return a.source.CostTier - b.source.CostTier
		}
		if hasPreferred && (a.channel.Id == preferred) != (b.channel.Id == preferred) {
			if a.channel.Id == preferred {
				return -1
			}
			return 1
		}
		if a.rank < b.rank {
			return -1
		}
		return 1
	})
	for _, candidate := range candidates {
		if err = reserveUpstream(c, p, candidate.account, candidate.source, time.Now()); err != nil {
			RequestPolicy(c).AddEvent(PolicyEvent{ChannelID: candidate.channel.Id, Decision: PolicyDecision{Action: "skip", Reason: err.Error(), Source: "upstream_capacity"}})
			continue
		}
		if param.TokenGroup == "auto" {
			common.SetContextKey(c, constant.ContextKeyAutoGroup, candidate.group)
		}
		RequestPolicy(c).AddEvent(PolicyEvent{ChannelID: candidate.channel.Id, Decision: PolicyDecision{Action: "select", Reason: "upstream_capacity_reserved", Source: "upstream_routing"}})
		return candidate.channel, candidate.group, true, nil
	}
	return nil, "", true, errors.New(i18n.T(c, i18n.MsgUpstreamRoutingCapacityUnavailable))
}

func upstreamFailure(err *types.NewAPIError) (string, bool) {
	if err == nil {
		return "", false
	}
	text := strings.ToLower(string(err.GetErrorCode()) + " " + err.Error())
	if err.StatusCode == 401 || strings.Contains(text, "invalid_api_key") || strings.Contains(text, "invalid api key") {
		return "invalid_credentials", true
	}
	for _, hint := range []string{"insufficient_quota", "billing_hard_limit_reached", "quota exhausted", "insufficient balance", "balance is insufficient", "credit balance", "余额不足"} {
		if strings.Contains(text, hint) {
			return "upstream_quota_exhausted", true
		}
	}
	if err.StatusCode == 402 {
		return "upstream_quota_exhausted", true
	}
	if err.StatusCode == 429 {
		return "rate_limit", false
	}
	if err.StatusCode == 404 {
		return "model_unavailable", false
	}
	if err.StatusCode >= 500 || err.StatusCode == 0 || err.GetErrorCode() == types.ErrorCodeDoRequestFailed {
		return "temporary_failure", false
	}
	return "request_rejected", false
}
func upstreamRetryAfter(value string, now time.Time) int64 {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && seconds >= 0 {
		return now.Unix() + max(1, min(seconds, int64(7*24*3600)))
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Unix()
	}
	return 0
}

// FinishUpstreamAttempt settles capacity once. A nil outcome means successful
// upstream completion; CloseUpstreamRequest handles abandoned/local failures.
func FinishUpstreamAttempt(c *gin.Context, apiErr *types.NewAPIError, success bool) {
	v, ok := c.Get(upstreamRequestKey)
	if !ok {
		return
	}
	p := v.(*upstreamRequest)
	at := p.Current
	if at == nil {
		return
	}
	p.Current = nil
	at.Once.Do(func() { close(at.Stop) })
	now := time.Now()
	if !success && apiErr == nil && at.Sent && c.Request.Context().Err() == nil {
		apiErr = types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgUpstreamRoutingStreamIncomplete)), types.ErrorCodeBadResponse, 502)
	}
	reason, accountFault := upstreamFailure(apiErr)
	input, inputOK := c.Get("upstream_usage_input")
	output, outputOK := c.Get("upstream_usage_output")
	actual := at.Estimate
	known := success && inputOK && outputOK
	if known {
		in, ok1 := input.(int64)
		out, ok2 := output.(int64)
		known = ok1 && ok2 && in >= 0 && out >= 0 && in <= 1e12 && out <= 1e12
		if known {
			actual.Tokens = float64(in + out)
			actual.USD = at.Source.RequestUSD + (float64(in)*at.Source.InputUSDPerMillion+float64(out)*at.Source.OutputUSDPerMillion)/1e6
		}
	}
	c.Set("upstream_usage_input", nil)
	c.Set("upstream_usage_output", nil)
	// A definitive upstream rejection consumes RPM but not generation tokens or
	// money; transport/5xx/cancellation may have incurred cost, so keep estimates.
	if !success && (reason == "rate_limit" || reason == "model_unavailable" || accountFault || apiErr != nil && apiErr.StatusCode == 400) {
		actual.Tokens = 0
		actual.USD = 0
		known = true
	}
	if !success && apiErr == nil && !at.Sent {
		actual = UpstreamUsage{}
		known = true
	}
	delta := UpstreamUsage{Requests: actual.Requests - at.Estimate.Requests, Tokens: actual.Tokens - at.Estimate.Tokens, USD: actual.USD - at.Estimate.USD}
	err := model.UpdateUpstreamRoutingRecords(at.Scopes, func(rows map[string]*model.UpstreamRoutingRecord) error {
		for i, scope := range at.Scopes {
			s, e := decodeUpstreamState(rows[scope], now)
			if e != nil {
				return e
			}
			delete(s.Leases, at.ID)
			if i != 0 || s.Period == at.Period {
				s.Usage.add(delta)
			}
			s.Lifetime.add(delta)
			if actual.Requests == 0 {
				s.Events = slices.DeleteFunc(s.Events, func(e UpstreamRateEvent) bool { return e.ID == at.ID })
			}
			for j := range s.Events {
				if s.Events[j].ID == at.ID {
					s.Events[j].Tokens = int64(actual.Tokens)
				}
			}
			if known {
				s.EstimatedRequests = max(0, s.EstimatedRequests-1)
			}
			s.Estimated = s.EstimatedRequests > 0
			if i == 0 && s.BalanceEpoch == at.Account.BalanceUpdatedAt {
				s.BalanceUsed = max(0, s.BalanceUsed+delta.USD)
			}
			if success {
				s.Successes++
				s.Failures = 0
				if s.Recovering && s.CooldownUntil <= now.Unix() {
					s.RecoverySuccesses++
					if s.RecoverySuccesses >= 3 {
						s.Recovering = false
						s.Backoff = 0
						s.CooldownUntil = 0
					}
				}
			} else if apiErr != nil {
				s.Errors++
				if i == 0 && accountFault {
					s.Paused = reason
				}
				if reason == "rate_limit" || i == 1 && reason == "temporary_failure" {
					s.Failures++
					if reason == "rate_limit" || s.Failures >= p.Config.FailureThreshold {
						seconds := []int{30, 60, 120, 300}[min(s.Backoff, 3)]
						if reason != "rate_limit" {
							seconds = max(seconds, p.Config.CooldownSeconds)
						}
						until := now.Unix() + int64(seconds)
						if reason == "rate_limit" {
							if retry := upstreamRetryAfter(c.GetString(UpstreamRetryAfterKey), now); retry > 0 {
								until = retry
							}
						}
						s.CooldownUntil = until
						s.Backoff = min(s.Backoff+1, 3)
						s.Recovering = true
						s.RecoverySuccesses = 0
					}
				}
				if i == 1 && reason == "model_unavailable" {
					s.Paused = reason
				}
			}
			if e = encodeUpstreamState(rows[scope], s); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		common.SysError("upstream capacity settlement failed; conservative reservation retained")
	}
	if apiErr != nil {
		RequestPolicy(c).AddEvent(PolicyEvent{ChannelID: at.Source.ChannelID, Decision: PolicyDecision{Action: "isolate", Reason: reason, Source: "upstream_routing"}})
	}
}

// MarkUpstreamAttemptStarted distinguishes local validation failures from
// ambiguous submissions that may already have incurred an upstream charge.
func MarkUpstreamAttemptStarted(c *gin.Context) {
	if v, ok := c.Get(upstreamRequestKey); ok {
		if at := v.(*upstreamRequest).Current; at != nil {
			at.Sent = true
		}
	}
}

func CloseUpstreamRequest(c *gin.Context) { FinishUpstreamAttempt(c, nil, false) }
func UpstreamRetryDecision(c *gin.Context, err *types.NewAPIError) (PolicyDecision, bool) {
	if !IsUpstreamManaged(c) {
		return PolicyDecision{}, false
	}
	p := c.MustGet(upstreamRequestKey).(*upstreamRequest)
	stop := func(reason string) (PolicyDecision, bool) {
		return PolicyDecision{Action: "stop", Reason: reason, Source: "upstream_routing"}, true
	}
	if c.Writer.Written() {
		return stop("response_already_started")
	}
	path := c.Request.URL.Path
	if strings.Contains(path, "images") || strings.Contains(path, "video") || strings.Contains(path, "/tasks") || strings.Contains(path, "/mj/") {
		if err.StatusCode != 429 && err.StatusCode != 401 && err.StatusCode != 402 && err.StatusCode != 404 {
			return stop("ambiguous_media_submission")
		}
	}
	if p.Attempts >= p.Config.MaxAttempts {
		return stop("attempt_budget_exhausted")
	}
	_, hasPin, _ := GetChannelConstraints(c).ResolvedPin()
	if hasPin || GetChannelConstraints(c).SuppressesRetry() || ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return stop("pinned_channel")
	}
	if types.IsSkipRetryError(err) {
		return stop("non_retryable_error")
	}
	reason, _ := upstreamFailure(err)
	if reason == "request_rejected" {
		return stop("status_not_retryable")
	}
	return PolicyDecision{Action: "retry", Reason: reason, Source: "upstream_routing"}, true
}

// ReserveUpstreamProbe admits an administrator's explicit channel test without
// requiring prior model verification. Tests still consume shared capacity and
// budget, and never change the manual channel switch or verification flag.
func ReserveUpstreamProbe(c *gin.Context, channelID int, name string) error {
	p, err := upstreamPolicy(c, name)
	if err != nil {
		return err
	}
	if !p.Managed {
		return nil
	}
	for _, a := range p.Config.Accounts {
		if !slices.Contains(a.ChannelIDs, channelID) {
			continue
		}
		source := dto.UpstreamSource{ChannelID: channelID, Weight: 1}
		for _, pool := range p.Config.Models {
			if pool.Model == name {
				for _, s := range pool.Sources {
					if s.ChannelID == channelID {
						source = s
						break
					}
				}
				break
			}
		}
		if !source.CostKnown && (a.Balance != nil || a.Budget.Unit == "usd" && a.Budget.Amount > 0) {
			return errors.New("Configure upstream unit costs before testing a monetary-limited account")
		}
		return reserveUpstream(c, p, a, source, time.Now())
	}
	return nil
}

func RecoverUpstreamScope(scope string, resetBudget bool) error {
	if !strings.HasPrefix(scope, "account:") && !strings.HasPrefix(scope, "source:") {
		return errors.New("Invalid upstream state scope")
	}
	return model.UpdateUpstreamRoutingRecords([]string{scope}, func(rows map[string]*model.UpstreamRoutingRecord) error {
		s, err := decodeUpstreamState(rows[scope], time.Now())
		if err != nil {
			return err
		}
		if resetBudget && len(s.Leases) > 0 {
			return errors.New("Wait for active upstream requests before resetting consumption")
		}
		if resetBudget {
			s.Usage = UpstreamUsage{}
			s.Lifetime = UpstreamUsage{}
			s.BalanceUsed = 0
			s.EstimatedRequests = 0
			s.Estimated = false
		} else {
			s.Paused = ""
			s.CooldownUntil = 0
			s.Failures = 0
			s.Backoff = 0
			s.Recovering = true
			s.RecoverySuccesses = 0
		}
		return encodeUpstreamState(rows[scope], s)
	})
}
