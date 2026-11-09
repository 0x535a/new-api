package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func upstreamFixture(t *testing.T) (dto.UpstreamRoutingConfig, func(string) *gin.Context) {
	t.Helper()
	require.NoError(t, i18n.Init())
	oldDB, oldCache := model.DB, common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.UpstreamRoutingRecord{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { model.DB = oldDB; common.MemoryCacheEnabled = oldCache; require.NoError(t, sqlDB.Close()) })
	for id := range 3 {
		n := id + 1
		require.NoError(t, db.Create(&model.Channel{Id: n, Name: fmt.Sprint(n), Type: 1, Status: 1, Models: "test-model,other-model", Group: "default", Key: "test"}).Error)
		for _, name := range []string{"test-model", "other-model"} {
			require.NoError(t, db.Create(&model.Ability{ChannelId: n, Model: name, Group: "default", Enabled: true}).Error)
		}
	}
	model.InitChannelCache()
	cfg := model.DefaultUpstreamRoutingConfig()
	cfg.Enabled = true
	cfg.Accounts = []dto.UpstreamAccount{{ID: "a", Name: "A", ChannelIDs: []int{1, 2}, Limits: dto.UpstreamLimits{RPM: 30, Concurrency: 1}}, {ID: "b", Name: "B", ChannelIDs: []int{3}, Limits: dto.UpstreamLimits{RPM: 30, Concurrency: 1}}}
	cfg.Models = []dto.UpstreamModelPool{{Model: "test-model", Enabled: true, Sources: []dto.UpstreamSource{{ChannelID: 1, Enabled: true, Verified: true, Weight: 1, Priority: 2}, {ChannelID: 2, Enabled: true, Verified: true, Weight: 1, Priority: 1}, {ChannelID: 3, Enabled: true, Verified: true, Weight: 1}}}, {Model: "other-model", Enabled: true, Sources: []dto.UpstreamSource{{ChannelID: 2, Enabled: true, Verified: true, Weight: 1}}}}
	cfg, err = model.SaveUpstreamRoutingConfig(cfg)
	require.NoError(t, err)
	return cfg, func(name string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"max_tokens":8}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Accept-Language", "en")
		t.Cleanup(func() { CloseUpstreamRequest(c); common.CleanupBodyStorage(c) })
		return c
	}
}
func selectUpstreamTest(c *gin.Context, name string) (*model.Channel, error) {
	ch, _, _, err := SelectUpstreamChannel(&RetryParam{Ctx: c, ModelName: name, TokenGroup: "default"}, nil)
	return ch, err
}
func upstreamStateTest(t *testing.T, scope string) UpstreamRuntime {
	t.Helper()
	var r model.UpstreamRoutingRecord
	require.NoError(t, model.DB.First(&r, "scope = ?", scope).Error)
	s, err := decodeUpstreamState(&r, time.Now())
	require.NoError(t, err)
	return s
}
func TestUpstreamAccountCapacityAndSettlement(t *testing.T) {
	_, ctx := upstreamFixture(t)
	first := ctx("test-model")
	ch, err := selectUpstreamTest(first, "test-model")
	require.NoError(t, err)
	assert.Equal(t, 1, ch.Id)
	// A second channel cannot bypass a shared account's concurrency limit.
	_, err = selectUpstreamTest(ctx("other-model"), "other-model")
	require.Error(t, err)
	second := ctx("test-model")
	ch, err = selectUpstreamTest(second, "test-model")
	require.NoError(t, err)
	assert.Equal(t, 3, ch.Id)
	first.Set("upstream_usage_input", int64(4))
	first.Set("upstream_usage_output", int64(2))
	FinishUpstreamAttempt(first, nil, true)
	state := upstreamStateTest(t, "account:a")
	assert.Empty(t, state.Leases)
	assert.Equal(t, float64(6), state.Usage.Tokens)
	// Duplicate completion is harmless, and manual recovery preserves consumption.
	FinishUpstreamAttempt(first, nil, true)
	require.NoError(t, RecoverUpstreamScope("account:a", false))
	assert.Equal(t, state.Usage, upstreamStateTest(t, "account:a").Usage)
	third := ctx("other-model")
	_, err = selectUpstreamTest(third, "other-model")
	require.NoError(t, err)
	require.Error(t, RecoverUpstreamScope("account:a", true))
	FinishUpstreamAttempt(third, nil, true)
	require.NoError(t, RecoverUpstreamScope("account:a", true))
	assert.Zero(t, upstreamStateTest(t, "account:a").Usage.Tokens)
}
func TestUpstreamFaultIsolationAndRetrySafety(t *testing.T) {
	cfg, ctx := upstreamFixture(t)
	first := ctx("test-model")
	_, err := selectUpstreamTest(first, "test-model")
	require.NoError(t, err)
	missing := types.NewErrorWithStatusCode(errors.New("model not found"), types.ErrorCodeBadResponseStatusCode, 404)
	FinishUpstreamAttempt(first, missing, false)
	assert.Equal(t, "model_unavailable", upstreamStateTest(t, UpstreamSourceScope(1, "test-model")).Paused)
	assert.Empty(t, upstreamStateTest(t, "account:a").Paused)
	other := ctx("other-model")
	ch, err := selectUpstreamTest(other, "other-model")
	require.NoError(t, err)
	assert.Equal(t, 2, ch.Id)
	rate := types.NewErrorWithStatusCode(errors.New("too many requests"), types.ErrorCodeBadResponseStatusCode, 429)
	other.Set(UpstreamRetryAfterKey, "90")
	before := time.Now().Unix()
	FinishUpstreamAttempt(other, rate, false)
	state := upstreamStateTest(t, "account:a")
	assert.GreaterOrEqual(t, state.CooldownUntil, before+90)
	_, err = selectUpstreamTest(ctx("other-model"), "other-model")
	require.Error(t, err)
	// Same request uses a different source, never one beyond its attempt cap.
	ch, err = selectUpstreamTest(first, "test-model")
	require.NoError(t, err)
	assert.Equal(t, 3, ch.Id)
	first.Writer.WriteHeaderNow()
	decision, handled := UpstreamRetryDecision(first, rate)
	assert.True(t, handled)
	assert.Equal(t, "response_already_started", decision.Reason)
	media := ctx("test-model")
	media.Request.URL.Path = "/v1/images/generations"
	media.Set(upstreamRequestKey, &upstreamRequest{Managed: true, Config: cfg})
	decision, _ = UpstreamRetryDecision(media, types.NewErrorWithStatusCode(errors.New("timeout"), types.ErrorCodeDoRequestFailed, 502))
	assert.Equal(t, "ambiguous_media_submission", decision.Reason)
}
func TestUpstreamBudgetAndPendingModels(t *testing.T) {
	cfg, ctx := upstreamFixture(t)
	cfg.Accounts[0].Budget = dto.UpstreamBudget{Amount: 2, Reserve: 1, Unit: "requests", Period: "day", Timezone: "Asia/Singapore"}
	cfg.Models[0].Sources = cfg.Models[0].Sources[:1]
	_, err := model.SaveUpstreamRoutingConfig(cfg)
	require.NoError(t, err)
	c := ctx("test-model")
	_, err = selectUpstreamTest(c, "test-model")
	require.NoError(t, err)
	FinishUpstreamAttempt(c, nil, true)
	_, err = selectUpstreamTest(ctx("test-model"), "test-model")
	require.Error(t, err)
	_, err = selectUpstreamTest(ctx("new-model"), "new-model")
	require.ErrorContains(t, err, "pending")
	gpt := ctx("gpt-6-sol")
	_, _, handled, err := SelectUpstreamChannel(&RetryParam{Ctx: gpt, ModelName: "gpt-6-sol"}, nil)
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestUpstreamRecoveryPreservesManualDisableAndKillSwitch(t *testing.T) {
	cfg, ctx := upstreamFixture(t)
	c := ctx("test-model")
	_, err := selectUpstreamTest(c, "test-model")
	require.NoError(t, err)
	invalid := types.NewErrorWithStatusCode(errors.New("invalid_api_key"), types.ErrorCodeBadResponseStatusCode, 401)
	FinishUpstreamAttempt(c, invalid, false)
	assert.Equal(t, "invalid_credentials", upstreamStateTest(t, "account:a").Paused)
	// Pausing one credential also protects sibling channels, but not account B.
	next := ctx("test-model")
	ch, err := selectUpstreamTest(next, "test-model")
	require.NoError(t, err)
	assert.Equal(t, 3, ch.Id)
	FinishUpstreamAttempt(next, nil, true)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{1, 2}).Update("status", common.ChannelStatusManuallyDisabled).Error)
	model.InitChannelCache()
	require.NoError(t, RecoverUpstreamScope("account:a", false))
	ch, err = selectUpstreamTest(ctx("test-model"), "test-model")
	require.NoError(t, err)
	assert.Equal(t, 3, ch.Id)
	var disabled model.Channel
	require.NoError(t, model.DB.First(&disabled, 1).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, disabled.Status)
	cfg.Enabled = false
	_, err = model.SaveUpstreamRoutingConfig(cfg)
	require.NoError(t, err)
	_, _, handled, err := SelectUpstreamChannel(&RetryParam{Ctx: ctx("test-model"), ModelName: "test-model", TokenGroup: "default"}, nil)
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestUpstreamExplicitProbeSharesLimitsWithoutEnablingSource(t *testing.T) {
	cfg, ctx := upstreamFixture(t)
	cfg.Models[0].Sources[0].Enabled = false
	cfg.Models[0].Sources[0].Verified = false
	_, err := model.SaveUpstreamRoutingConfig(cfg)
	require.NoError(t, err)
	probe := ctx("test-model")
	require.NoError(t, ReserveUpstreamProbe(probe, 1, "test-model"))
	_, err = selectUpstreamTest(ctx("other-model"), "other-model")
	require.Error(t, err)
	FinishUpstreamAttempt(probe, nil, true)
	saved, err := model.ReadUpstreamRoutingConfig()
	require.NoError(t, err)
	assert.False(t, saved.Models[0].Sources[0].Enabled)
	assert.False(t, saved.Models[0].Sources[0].Verified)
}

func TestUpstreamAliasesCannotBypassModelCapacity(t *testing.T) {
	cfg, ctx := upstreamFixture(t)
	cfg.Accounts[0].Limits = dto.UpstreamLimits{}
	cfg.Models[0].Sources[0].Limits.Concurrency = 1
	cfg.Models = append(cfg.Models, dto.UpstreamModelPool{Model: "alias-model", Enabled: true, Sources: []dto.UpstreamSource{{ChannelID: 1, Model: "test-model", Enabled: true, Verified: true, Weight: 1}}})
	_, err := model.SaveUpstreamRoutingConfig(cfg)
	require.NoError(t, err)
	first := ctx("test-model")
	ch, err := selectUpstreamTest(first, "test-model")
	require.NoError(t, err)
	assert.Equal(t, 1, ch.Id)
	_, err = selectUpstreamTest(ctx("alias-model"), "alias-model")
	require.Error(t, err)
	FinishUpstreamAttempt(first, nil, true)
	ch, err = selectUpstreamTest(ctx("alias-model"), "alias-model")
	require.NoError(t, err)
	assert.Equal(t, 1, ch.Id)
}
