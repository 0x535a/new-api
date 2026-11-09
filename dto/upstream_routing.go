package dto

// UpstreamLimits apply to attempts sent upstream, independently of user billing.
type UpstreamLimits struct {
	RPM         int   `json:"rpm"`
	TPM         int64 `json:"tpm"`
	Concurrency int   `json:"concurrency"`
}

type UpstreamBudget struct {
	Amount   float64 `json:"amount"`
	Reserve  float64 `json:"reserve"`
	Unit     string  `json:"unit"`
	Period   string  `json:"period"`
	Timezone string  `json:"timezone"`
}

type UpstreamAccount struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ChannelIDs []int          `json:"channel_ids"`
	Paused     bool           `json:"paused"`
	Limits     UpstreamLimits `json:"limits"`
	Budget     UpstreamBudget `json:"budget"`
	// Balance is explicitly supplied upstream credit, never the gateway wallet.
	Balance          *float64 `json:"balance"`
	BalanceSource    string   `json:"balance_source"`
	BalanceUpdatedAt int64    `json:"balance_updated_at"`
}

type UpstreamSource struct {
	ChannelID           int            `json:"channel_id"`
	Model               string         `json:"model"` // Optional exposed name on this channel.
	Enabled             bool           `json:"enabled"`
	Verified            bool           `json:"verified"`
	Priority            int            `json:"priority"`
	Weight              int            `json:"weight"`
	CostTier            int            `json:"cost_tier"`
	CostKnown           bool           `json:"cost_known"`
	Limits              UpstreamLimits `json:"limits"`
	InputUSDPerMillion  float64        `json:"input_usd_per_million"`
	OutputUSDPerMillion float64        `json:"output_usd_per_million"`
	RequestUSD          float64        `json:"request_usd"`
}

type UpstreamModelPool struct {
	Model   string           `json:"model"`
	Enabled bool             `json:"enabled"`
	Sources []UpstreamSource `json:"sources"`
}

type UpstreamRoutingConfig struct {
	Revision         int64               `json:"revision"`
	Enabled          bool                `json:"enabled"`
	MaxAttempts      int                 `json:"max_attempts"`
	FailureThreshold int                 `json:"failure_threshold"`
	CooldownSeconds  int                 `json:"cooldown_seconds"`
	Accounts         []UpstreamAccount   `json:"accounts"`
	Models           []UpstreamModelPool `json:"models"`
}
