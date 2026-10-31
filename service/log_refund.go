package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var logRefundSchedulerOnce sync.Once

// logRefundBackfillDays 自动补账最多回溯的天数。停机跨过凌晨、或某天结算时
// 日志尚未落库，重启后逐天补回来。
const logRefundBackfillDays = 7

// StartLogRefundScheduler 启动异常返费的每日自动结算定时器（仅 master 节点）。
//
// 行为：每天北京时间 00:00:30 结算「刚结束的那一天」的完整数据
// （例如今天是 10-14 00:00:30，则结算 10-13 00:00:00 ~ 10-13 23:59:59）。
//
// 为什么不卡在 00:00:00 整点：凌晨零点系统有多个周期性任务同时启动
// （选项同步、看板刷新、额度重置等），容易造成竞争或干扰。
// 延迟 30 秒可以完全避开这些系统繁忙期。
//
// 为什么不直接执行而改入队系统任务：直接跑结算和系统任务框架（若 handler
// 存在）会形成两条并行执行路径，多节点部署时无法用 DB lease 去重。改为只负责
// 创建任务行，执行统一交给系统任务 runner，天然具备 lease 去重 + 失败重试。
//
// 启动时的行为：启动即扫描最近 logRefundBackfillDays 天，把漏掉结算的日期
// 逐天补账；已经结算过的日期（log_refund_runs 的 auto 记录保证幂等）自动跳过。
func StartLogRefundScheduler() {
	logRefundSchedulerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		go func() {
			// defer recover：goroutine panic 会让整个进程内的定时器永久死亡
			//（外层 for 循环挂掉），之后每天凌晨都不会再结算。包一层 recover，
			// 打日志后继续等下一个凌晨，结算本身有幂等保障，重复跑不会多退。
			defer func() {
				if r := recover(); r != nil {
					common.SysError(fmt.Sprintf("log refund scheduler panicked: %v", r))
				}
			}()
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

// runLogRefundAutoSettlement 检查开关状态，把最近 logRefundBackfillDays 天里漏掉
// 结算的日期逐天入队系统任务，由系统任务 runner 统一执行（DB lease 去重 + 失败重试）。
// 已经完整结算过的日期通过 HasPendingLogRefundAutoRunByDate 判断跳过，重复入队不会
// 重复退款（log_refund_records 唯一索引兜底）。active_key 去重决定了同一时刻只有
// 一张返费任务，多天补账由 EnqueueNextLogRefundBackfill 在每张任务完成后接力串起。
func runLogRefundAutoSettlement() {
	config, err := model.GetLogRefundConfig()
	if err != nil {
		common.SysError("log refund scheduler: " + err.Error())
		return
	}
	if !config.Enabled {
		return
	}
	// 从最旧到最新逐天检查，保证补账顺序
	for _, targetDate := range logRefundBackfillDates(config, time.Now()) {
		if !model.HasPendingLogRefundAutoRunByDate(targetDate) {
			continue
		}
		task, created, err := EnqueueSystemTask(model.SystemTaskTypeLogRefund, model.LogRefundPayload{Date: targetDate, Trigger: "auto"})
		if err != nil {
			common.SysError(fmt.Sprintf("log refund scheduler: enqueue %s failed: %v", targetDate, err))
			continue
		}
		if created {
			common.SysLog(fmt.Sprintf("log refund scheduler: enqueued settlement task for %s (task_id=%s)", targetDate, task.TaskID))
		}
	}
}

// logRefundBackfillDates 返回补账窗口内的日期（从旧到新）：最近 logRefundBackfillDays
// 个已结束的自然日，且不早于功能启用当天（README 承诺的行为，避免刚开启功能就把
// 启用前的历史异常也一股脑退掉）。UpdatedAt 在每次开关切换时刷新，窗口以最近一次
// 开启时间为准。
func logRefundBackfillDates(config *model.LogRefundConfig, now time.Time) []string {
	beijing := now.In(model.BeijingLocation())
	enabledSince := ""
	if config != nil && config.UpdatedAt > 0 {
		enabledSince = time.Unix(config.UpdatedAt, 0).In(model.BeijingLocation()).Format("2006-01-02")
	}
	dates := make([]string, 0, logRefundBackfillDays)
	for i := logRefundBackfillDays; i >= 1; i-- {
		date := beijing.AddDate(0, 0, -i).Format("2006-01-02")
		if enabledSince != "" && date < enabledSince {
			continue
		}
		dates = append(dates, date)
	}
	return dates
}

// EnqueueNextLogRefundBackfill 一张自动结算任务完成后，把窗口内下一个「从未尝试过」
// 的漏结算日期串起来入队，多个漏结算日无需等到第二天凌晨，几分钟内就能逐天补完
//（runner 空闲轮询 15 秒一张）。只串从未尝试过的日期：失败/截断过的日期留给每日
// 00:00:30 的定时器重试（天然限频一天一次），避免确定性失败（如钱包上限）在分钟级
// 形成快速重试死循环。管理端手动触发的任务不串链，由管理员自主决定。
func EnqueueNextLogRefundBackfill() {
	config, err := model.GetLogRefundConfig()
	if err != nil {
		common.SysError("log refund backfill: " + err.Error())
		return
	}
	if !config.Enabled {
		return
	}
	for _, targetDate := range logRefundBackfillDates(config, time.Now()) {
		processed, err := model.LogRefundDateProcessed(targetDate)
		if err != nil {
			common.SysError("log refund backfill: " + err.Error())
			return
		}
		if processed {
			continue
		}
		attempted, err := model.LogRefundDateAttempted(targetDate)
		if err != nil {
			common.SysError("log refund backfill: " + err.Error())
			return
		}
		if attempted {
			continue
		}
		task, created, err := EnqueueSystemTask(model.SystemTaskTypeLogRefund, model.LogRefundPayload{Date: targetDate, Trigger: "auto"})
		if err != nil {
			common.SysError(fmt.Sprintf("log refund backfill: enqueue %s failed: %v", targetDate, err))
			return
		}
		if created {
			common.SysLog(fmt.Sprintf("log refund backfill: enqueued settlement task for %s (task_id=%s)", targetDate, task.TaskID))
		}
		return
	}
}
