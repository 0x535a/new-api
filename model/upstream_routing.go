package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// Routing records keep configuration and capacity reservations separate from
// channel status and user accounting. TEXT is portable to all supported DBs.
type UpstreamRoutingRecord struct {
	Scope     string                  `json:"scope" gorm:"type:varchar(191);primaryKey"`
	Document  UpstreamRoutingDocument `json:"document" gorm:"not null"`
	UpdatedAt int64                   `json:"updated_at"`
}

// MySQL TEXT is limited to 64 KiB, too small for a model directory.
type UpstreamRoutingDocument string

func (UpstreamRoutingDocument) GormDataType() string { return "text" }
func (UpstreamRoutingDocument) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "LONGTEXT"
	}
	return "TEXT"
}
func (d UpstreamRoutingDocument) Value() (driver.Value, error) { return string(d), nil }
func (d *UpstreamRoutingDocument) Scan(v any) error {
	switch x := v.(type) {
	case string:
		*d = UpstreamRoutingDocument(x)
	case []byte:
		*d = UpstreamRoutingDocument(x)
	default:
		return errors.New("Invalid upstream routing document")
	}
	return nil
}

var upstreamRoutingMutex sync.Mutex
var ErrUpstreamRoutingConflict = errors.New("Upstream routing configuration changed; reload before saving")

func DefaultUpstreamRoutingConfig() dto.UpstreamRoutingConfig {
	return dto.UpstreamRoutingConfig{MaxAttempts: 3, FailureThreshold: 3, CooldownSeconds: 30, Accounts: []dto.UpstreamAccount{}, Models: []dto.UpstreamModelPool{}}
}

func IsUnmanagedGPTModel(name string) bool {
	name = strings.ToLower(name)
	if _, tail, ok := strings.Cut(name, "/"); ok {
		name = tail
	}
	return strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "chatgpt-") || strings.HasPrefix(name, "codex-") || strings.HasPrefix(name, "o1") || strings.HasPrefix(name, "o3") || strings.HasPrefix(name, "o4") || name == "chat-latest"
}

func ReadUpstreamRoutingConfig() (dto.UpstreamRoutingConfig, error) {
	cfg := DefaultUpstreamRoutingConfig()
	var row UpstreamRoutingRecord
	err := DB.First(&row, "scope = ?", "config").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	err = common.UnmarshalJsonStr(string(row.Document), &cfg)
	return cfg, err
}

func ValidateUpstreamRoutingConfig(cfg dto.UpstreamRoutingConfig) error {
	if cfg.MaxAttempts < 1 || cfg.MaxAttempts > 3 || cfg.FailureThreshold < 1 || cfg.FailureThreshold > 20 || cfg.CooldownSeconds < 1 || cfg.CooldownSeconds > 300 {
		return errors.New("Invalid routing attempt or cooldown limits")
	}
	if len(cfg.Accounts) > 1000 || len(cfg.Models) > 10000 {
		return errors.New("Too many upstream routing rules")
	}
	ids := map[string]bool{}
	channels := map[int]string{}
	validID := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	for _, a := range cfg.Accounts {
		if !validID.MatchString(a.ID) || strings.TrimSpace(a.Name) == "" || len(a.Name) > 128 || ids[a.ID] {
			return errors.New("Account identifiers must be unique and names must not be empty")
		}
		ids[a.ID] = true
		if err := ValidateUpstreamLimits(a.Limits); err != nil {
			return err
		}
		b := a.Budget
		if math.IsNaN(b.Amount) || math.IsInf(b.Amount, 0) || math.IsNaN(b.Reserve) || math.IsInf(b.Reserve, 0) || b.Amount < 0 || b.Amount > 1e12 || b.Reserve < 0 || b.Reserve > b.Amount {
			return errors.New("Invalid upstream budget")
		}
		if !slices.Contains([]string{"", "requests", "tokens", "usd"}, b.Unit) || !slices.Contains([]string{"", "day", "month", "lifetime"}, b.Period) {
			return errors.New("Invalid budget unit or reset period")
		}
		if b.Amount > 0 && (b.Unit == "" || b.Period == "") {
			return errors.New("A budget requires a unit and reset period")
		}
		if b.Timezone != "" {
			if _, err := time.LoadLocation(b.Timezone); err != nil {
				return errors.New("Invalid budget timezone")
			}
		}
		if a.Balance != nil && (math.IsNaN(*a.Balance) || math.IsInf(*a.Balance, 0) || *a.Balance < 0 || *a.Balance > 1e12) {
			return errors.New("Invalid upstream balance")
		}
		if a.Balance != nil && a.BalanceSource != "manual" {
			return errors.New("A supplied upstream balance must be labeled manual")
		}
		for _, id := range a.ChannelIDs {
			if id <= 0 || channels[id] != "" {
				return errors.New("Each channel must belong to exactly one upstream account")
			}
			var c Channel
			if err := DB.Select("id", "tag").First(&c, id).Error; err != nil {
				return fmt.Errorf("Channel %d does not exist", id)
			}
			if c.Tag != nil && *c.Tag == "GPT" {
				return errors.New("GPT channels are excluded from upstream routing")
			}
			channels[id] = a.ID
		}
	}
	models := map[string]bool{}
	for _, p := range cfg.Models {
		if strings.TrimSpace(p.Model) == "" || len(p.Model) > 255 || models[p.Model] || IsUnmanagedGPTModel(p.Model) {
			return errors.New("Model names must be unique and GPT models must remain unmanaged")
		}
		models[p.Model] = true
		seen := map[int]bool{}
		for _, s := range p.Sources {
			if len(s.Model) > 255 || IsUnmanagedGPTModel(s.Model) {
				return errors.New("Invalid source model")
			}
			if channels[s.ChannelID] == "" || seen[s.ChannelID] || s.Priority < 0 || s.Priority > 100000 || s.Weight < 1 || s.Weight > 100000 || s.CostTier < 0 || s.CostTier > 1000 {
				return errors.New("Invalid model source, weight, priority, or account")
			}
			seen[s.ChannelID] = true
			if err := ValidateUpstreamLimits(s.Limits); err != nil {
				return err
			}
			for _, price := range []float64{s.InputUSDPerMillion, s.OutputUSDPerMillion, s.RequestUSD} {
				if price < 0 || price > 1e6 || math.IsNaN(price) || math.IsInf(price, 0) {
					return errors.New("Invalid upstream unit cost")
				}
			}
			for _, a := range cfg.Accounts {
				if slices.Contains(a.ChannelIDs, s.ChannelID) && (a.Budget.Unit == "usd" && a.Budget.Amount > 0 || a.Balance != nil) && !s.CostKnown {
					return errors.New("Monetary limits require explicit upstream unit costs")
				}
			}
		}
	}
	return nil
}

func ValidateUpstreamLimits(l dto.UpstreamLimits) error {
	if l.RPM < 0 || l.RPM > 100000 || l.TPM < 0 || l.TPM > 1e12 || l.Concurrency < 0 || l.Concurrency > 100000 {
		return errors.New("Invalid upstream rate or concurrency limits")
	}
	return nil
}

// UpdateUpstreamRoutingRecords serializes SQLite writers locally and locks
// rows in a stable order on MySQL/Postgres. All reservations commit together.
func UpdateUpstreamRoutingRecords(scopes []string, update func(map[string]*UpstreamRoutingRecord) error) error {
	upstreamRoutingMutex.Lock()
	defer upstreamRoutingMutex.Unlock()
	scopes = slices.Clone(scopes)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	return DB.Transaction(func(tx *gorm.DB) error {
		records := map[string]*UpstreamRoutingRecord{}
		for _, scope := range scopes {
			row := UpstreamRoutingRecord{Scope: scope, Document: "{}"}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
			if err := lockForUpdate(tx).First(&row, "scope = ?", scope).Error; err != nil {
				return err
			}
			records[scope] = &row
		}
		if err := update(records); err != nil {
			return err
		}
		for _, row := range records {
			if err := tx.Model(&UpstreamRoutingRecord{}).Where("scope = ?", row.Scope).Updates(map[string]any{"document": row.Document, "updated_at": time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func SaveUpstreamRoutingConfig(cfg dto.UpstreamRoutingConfig) (dto.UpstreamRoutingConfig, error) {
	if err := ValidateUpstreamRoutingConfig(cfg); err != nil {
		return cfg, err
	}
	err := UpdateUpstreamRoutingRecords([]string{"config"}, func(rows map[string]*UpstreamRoutingRecord) error {
		current := DefaultUpstreamRoutingConfig()
		if err := common.UnmarshalJsonStr(string(rows["config"].Document), &current); err != nil {
			return err
		}
		if current.Revision != cfg.Revision {
			return ErrUpstreamRoutingConflict
		}
		cfg.Revision++
		b, err := common.Marshal(cfg)
		rows["config"].Document = UpstreamRoutingDocument(b)
		return err
	})
	return cfg, err
}

func ListUpstreamRoutingRecords() ([]UpstreamRoutingRecord, error) {
	var records []UpstreamRoutingRecord
	err := DB.Where("scope <> ?", "config").Find(&records).Error
	return records, err
}
