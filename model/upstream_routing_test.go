package model

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestUpstreamRoutingDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "upstream_matrix_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previous := DB
			DB = db
			t.Cleanup(func() {
				DB = previous
				require.NoError(t, db.Migrator().DropTable(&UpstreamRoutingRecord{}, &Channel{}))
				require.NoError(t, sqlDB.Close())
			})
			var version string
			q := "select version()"
			if dialect == "sqlite" {
				q = "select sqlite_version()"
			}
			require.NoError(t, db.Raw(q).Scan(&version).Error)
			t.Log("database", version)
			// Start with pre-feature channels, then migrate twice without altering them.
			require.NoError(t, db.AutoMigrate(&Channel{}))
			tag := "Mix"
			channel := Channel{Id: 1, Name: "existing-account", Type: 1, Key: "test-key", Status: 1, Models: "test-model", Group: "default", Tag: &tag}
			require.NoError(t, db.Create(&channel).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&UpstreamRoutingRecord{}))
			}
			require.NoError(t, db.Create(&UpstreamRoutingRecord{Scope: "large-directory", Document: UpstreamRoutingDocument(strings.Repeat("x", 100000))}).Error)
			var large UpstreamRoutingRecord
			require.NoError(t, db.First(&large, "scope = ?", "large-directory").Error)
			assert.Len(t, large.Document, 100000)
			var restored Channel
			require.NoError(t, db.First(&restored, 1).Error)
			assert.Equal(t, "test-key", restored.Key)
			cfg, err := ReadUpstreamRoutingConfig()
			require.NoError(t, err)
			assert.False(t, cfg.Enabled)
			cfg.Accounts = []dto.UpstreamAccount{{ID: "account-a", Name: "Account A", ChannelIDs: []int{1}}}
			cfg.Models = []dto.UpstreamModelPool{{Model: "test-model", Enabled: true, Sources: []dto.UpstreamSource{{ChannelID: 1, Enabled: true, Verified: true, Weight: 1}}}}
			cfg, err = SaveUpstreamRoutingConfig(cfg)
			require.NoError(t, err)
			assert.EqualValues(t, 1, cfg.Revision)
			stale := cfg
			stale.Revision = 0
			_, err = SaveUpstreamRoutingConfig(stale)
			assert.ErrorIs(t, err, ErrUpstreamRoutingConflict)
			got, err := ReadUpstreamRoutingConfig()
			require.NoError(t, err)
			assert.Equal(t, cfg, got)
			cfg.Models[0].Model = "gpt-6-sol"
			assert.Error(t, ValidateUpstreamRoutingConfig(cfg))
			cfg.Models[0].Model = "test-model"
			cfg.Accounts = append(cfg.Accounts, dto.UpstreamAccount{ID: "account-b", Name: "Account B", ChannelIDs: []int{1}})
			assert.Error(t, ValidateUpstreamRoutingConfig(cfg))
			err = UpdateUpstreamRoutingRecords([]string{"account:a", "source:1"}, func(records map[string]*UpstreamRoutingRecord) error {
				records["account:a"].Document = `{"requests":1}`
				records["source:1"].Document = `{"requests":1}`
				return nil
			})
			require.NoError(t, err)
			failure := errors.New("reservation rejected")
			err = UpdateUpstreamRoutingRecords([]string{"account:a", "source:1"}, func(records map[string]*UpstreamRoutingRecord) error {
				records["account:a"].Document = `{"requests":2}`
				return failure
			})
			assert.ErrorIs(t, err, failure)
			var row UpstreamRoutingRecord
			require.NoError(t, db.First(&row, "scope = ?", "account:a").Error)
			assert.JSONEq(t, `{"requests":1}`, string(row.Document))
			require.NoError(t, db.AutoMigrate(&UpstreamRoutingRecord{}))
			require.NoError(t, db.First(&row, "scope = ?", "account:a").Error)
			assert.JSONEq(t, `{"requests":1}`, string(row.Document))
		})
	}
}
