package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"sync"
	"time"
)

var lotterySchedulerOnce sync.Once

// StartLotteryScheduler 启动时补结算，随后精确对齐北京时间零点；数据库 lease 和
// 抽奖事务双层去重。常规系统调度器负责失败重试，不暴露手工指定开奖接口。
func StartLotteryScheduler() {
	lotterySchedulerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		go func() {
			for {
				if _, _, err := EnqueueSystemTask(model.SystemTaskTypeLotterySettlement, nil); err != nil {
					common.SysError("lottery scheduler: " + err.Error())
				}
				now := time.Now().In(model.BeijingLocation())
				midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, model.BeijingLocation())
				timer := time.NewTimer(time.Until(midnight))
				<-timer.C
			}
		}()
	})
}
