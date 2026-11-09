package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetUpstreamRouting(c *gin.Context) {
	cfg, err := model.ReadUpstreamRoutingConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	records, err := model.ListUpstreamRoutingRecords()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	states := map[string]service.UpstreamRuntime{}
	for _, r := range records {
		var s service.UpstreamRuntime
		if err = common.UnmarshalJsonStr(string(r.Document), &s); err != nil {
			common.ApiError(c, err)
			return
		}
		states[r.Scope] = s
	}
	// Only explicit safe metadata is returned. Channel credentials never enter
	// the routing editor response.
	var channels []struct {
		ID     int     `json:"id"`
		Name   string  `json:"name"`
		Status int     `json:"status"`
		Models string  `json:"models"`
		Tag    *string `json:"tag"`
	}
	if err = model.DB.Model(&model.Channel{}).Select("id", "name", "status", "models", "tag").Order("id").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	scopes := map[string]map[int]string{}
	for _, p := range cfg.Models {
		scopes[p.Model] = map[int]string{}
		for _, s := range p.Sources {
			scope := service.UpstreamSourceScope(s.ChannelID, p.Model)
			if _, ok := states[scope]; ok {
				scopes[p.Model][s.ChannelID] = scope
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"config": cfg, "states": states, "channels": channels, "source_scopes": scopes}})
}
func SaveUpstreamRouting(c *gin.Context) {
	var cfg dto.UpstreamRoutingConfig
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20), &cfg); err != nil {
		common.ApiErrorT(c, "Invalid upstream routing configuration")
		return
	}
	saved, err := model.SaveUpstreamRoutingConfig(cfg)
	if err != nil {
		if errors.Is(err, model.ErrUpstreamRoutingConflict) {
			c.Status(http.StatusConflict)
		}
		common.ApiErrorT(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": saved})
}
func OperateUpstreamRouting(c *gin.Context) {
	var action struct {
		Scope  string `json:"scope"`
		Action string `json:"action"`
	}
	if common.DecodeJson(c.Request.Body, &action) != nil || len(action.Scope) > 191 || (action.Action != "recover" && action.Action != "reset_budget") || !strings.HasPrefix(action.Scope, "account:") && !strings.HasPrefix(action.Scope, "source:") {
		common.ApiErrorT(c, "Invalid upstream state operation")
		return
	}
	records, err := model.ListUpstreamRoutingRecords()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	found := false
	for _, r := range records {
		if r.Scope == action.Scope {
			found = true
			break
		}
	}
	if !found {
		common.ApiErrorT(c, "Upstream state does not exist")
		return
	}
	if err = service.RecoverUpstreamScope(action.Scope, action.Action == "reset_budget"); err != nil {
		common.ApiErrorT(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
