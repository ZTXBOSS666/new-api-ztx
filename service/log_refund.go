package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var logRefundSchedulerOnce sync.Once

// StartLogRefundScheduler 启动异常返费的每日自动结算定时器（仅 master 节点）。
//
// 行为：每天北京时间 00:00:30 结算「刚结束的那一天」的完整数据
// （例如今天是 10-14 00:00:30，则结算 10-13 00:00:00 ~ 10-13 23:59:59）。
//
// 为什么不卡在 00:00:00 整点：凌晨零点系统有多个周期性任务同时启动
// （选项同步、看板刷新、额度重置等），容易造成竞争或干扰。
// 延迟 30 秒可以完全避开这些系统繁忙期。
//
// 为什么不用系统任务框架：自动返费只需要每天精确跑一次，不需要 DB lease 去重、
// 失败重试等复杂机制；goroutine + time.Sleep 就够了，跟抽奖调度器同一套路。
//
// 启动时的行为：如果「昨天」还没有 auto 结算记录，立即补结算一次；
// 如果程序在白天重启，已经结算过就不会重复执行（RefundAbnormalConsumeLogs
// 内部按 log_refund_records 的唯一索引保证幂等）。
func StartLogRefundScheduler() {
	logRefundSchedulerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		go func() {
			for {
				runLogRefundAutoSettlement()
				now := time.Now().In(model.BeijingLocation())
				next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 30, 0, model.BeijingLocation())
				timer := time.NewTimer(time.Until(next))
				<-timer.C
			}
		}()
	})
}

// runLogRefundAutoSettlement 检查开关状态，结算「昨天」的异常日志。
// 通过 log_refund_runs 的 auto 记录判断昨天是否已结算过，重复调用不会重复退款。
func runLogRefundAutoSettlement() {
	config, err := model.GetLogRefundConfig()
	if err != nil {
		common.SysError("log refund scheduler: " + err.Error())
		return
	}
	if !config.Enabled {
		return
	}
	if !model.HasPendingLogRefundAutoRun(time.Now()) {
		return
	}
	targetDate := model.LogRefundPreviousDate(time.Now())
	result, err := model.RefundAbnormalConsumeLogs(targetDate, time.Now(), "auto")
	if err != nil {
		common.SysError(fmt.Sprintf("log refund auto settlement failed for %s: %v", targetDate, err))
		return
	}
	common.SysLog(fmt.Sprintf("log refund auto settlement for %s: %d logs, %d users, %d quota",
		result.TargetDate, result.LogCount, result.UserCount, result.RefundedQuota))
}
