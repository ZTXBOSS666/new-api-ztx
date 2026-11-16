package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
				// recover 必须包在单次迭代内部：写在 goroutine 最外层的话，panic 之后
				// defer 一执行函数就返回了，循环根本活不到下一轮（退费那边就踩了这个坑）。
				// 结算有 DB lease + 事务去重，重复入队不会重复发奖，放心重试。
				func() {
					defer func() {
						if r := recover(); r != nil {
							common.SysError(fmt.Sprintf("lottery scheduler panicked: %v", r))
						}
					}()
					if _, _, err := EnqueueSystemTask(model.SystemTaskTypeLotterySettlement, nil); err != nil {
						common.SysError("lottery scheduler: " + err.Error())
					}
				}()
				now := time.Now().In(model.BeijingLocation())
				midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, model.BeijingLocation())
				timer := time.NewTimer(time.Until(midnight))
				<-timer.C
			}
		}()
	})
}
