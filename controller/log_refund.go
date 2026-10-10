package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type logRefundConfigRequest struct {
	Enabled bool `json:"enabled"`
}

// GetLogRefundAdmin 返回异常返费开关，以及指定日期（默认北京时间今天）的运行情况。
// 该接口仅管理员可用（路由已挂 AdminAuth）。
func GetLogRefundAdmin(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		date = model.LogRefundTargetDate(time.Now())
	}
	if err := model.ValidateLogRefundTargetDate(date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "返费日期格式无效"})
		return
	}
	config, err := model.GetLogRefundConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取返费配置失败"})
		return
	}
	dateProcessed, err := model.LogRefundDateProcessed(date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取返费运行记录失败"})
		return
	}
	dateRun, err := model.GetLogRefundRunByDate(date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取返费运行记录失败"})
		return
	}
	lastRun, err := model.GetLatestLogRefundRun()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取返费运行记录失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"enabled":        config.Enabled,
		"updated_at":     config.UpdatedAt,
		"date":           date,
		"date_processed": dateProcessed,
		"date_last_run":  dateRun,
		"last_run":       lastRun,
	}})
}

func UpdateLogRefundAdmin(c *gin.Context) {
	var request logRefundConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "返费配置格式无效"})
		return
	}
	config, err := model.UpdateLogRefundEnabled(request.Enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存返费配置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "返费配置已更新", "data": gin.H{
		"enabled":    config.Enabled,
		"updated_at": config.UpdatedAt,
	}})
}

// RunLogRefundAdmin 手动触发一次异常返费（一键返费）。仅管理员可用（路由已挂
// AdminAuth）。默认处理北京时间今天，可通过 date 指定其它日期（YYYY-MM-DD）。
//
// 同一天只要已经完整执行过一次返费（手动或凌晨自动），这里就直接拒绝，避免重复
// 点击；任务交给系统任务运行器异步执行，已经退过的日志本身也不会重复退还。
func RunLogRefundAdmin(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		date = model.LogRefundTargetDate(time.Now())
	}
	if err := model.ValidateLogRefundTargetDate(date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "返费日期格式无效"})
		return
	}
	processed, err := model.LogRefundDateProcessed(date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取返费运行记录失败"})
		return
	}
	if processed {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": date + " 已完成返费，不能重复执行"})
		return
	}
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeLogRefund, model.LogRefundPayload{Date: date, Trigger: "manual"})
	if err != nil || task == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "提交返费任务失败"})
		return
	}
	message := "返费任务已提交，正在执行"
	if !created {
		message = "已有返费任务在执行，已复用该任务"
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": message, "data": gin.H{
		"date":    date,
		"queued":  created,
		"task_id": task.TaskID,
	}})
}
