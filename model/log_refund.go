package model

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 异常日志返费：把「流状态异常」或「空返回（补全 token 为 0）」但仍然产生了扣费的
// 消费日志，按用户按天汇总退还额度，并在用户日志里写一条汇总提示。
//
// 幂等：log_refund_records 以原消费日志 ID 作为唯一索引，同一条消费日志只会退还
// 一次。写入前会跳过已存在的记录，重复运行（重试 / 手动一键返费）不会多退。
//
// 结算入口有两处：
//  1. 系统任务调度器：每天北京时间 0 点后自动对「刚结束的那一天」执行一次；
//  2. 管理端「一键返费」：立即对指定日期执行一次。
const (
	LogRefundConfigID = 1
	// logRefundBatchSize 每批扫描的日志条数。
	logRefundBatchSize = 500
	// maxLogRefundScanRows 单次运行最多扫描的候选行数：防御性上限，避免异常数据把
	// 内存打爆。触及上限时标记 truncated，调度器稍后会继续处理剩余部分。
	maxLogRefundScanRows = 200000
)

var (
	// ErrLogRefundInvalidDate 日期串不是合法的 YYYY-MM-DD。
	ErrLogRefundInvalidDate = errors.New("invalid log refund date")
	// errLogRefundWalletLimit 退还后余额会超过钱包上限，整批回滚（宁可不退也不越界）。
	errLogRefundWalletLimit = errors.New("log refund would exceed wallet quota limit")
)

// LogRefundConfig 异常返费开关，单行配置（ID 固定 1）。
// 注意：布尔默认值由 InitializeLogRefundConfig 显式写入，不使用 gorm default 标签，
// 避免 MySQL/PostgreSQL 对布尔默认值的归一化差异导致反复 ALTER TABLE。
type LogRefundConfig struct {
	Id        int   `json:"id" gorm:"primaryKey"`
	Enabled   bool  `json:"enabled"`
	UpdatedAt int64 `json:"updated_at"`
}

func (LogRefundConfig) TableName() string { return "log_refund_configs" }

// LogRefundRecord 每条已退还的消费日志；log_id 唯一索引是幂等的最后一道保障。
type LogRefundRecord struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	LogId      int    `json:"log_id" gorm:"not null;uniqueIndex:idx_log_refund_records_log_id"`
	UserId     int    `json:"user_id" gorm:"not null;index:idx_log_refund_records_user_id"`
	RefundDate string `json:"refund_date" gorm:"type:varchar(10);not null;index:idx_log_refund_records_refund_date"`
	Quota      int    `json:"quota" gorm:"not null"`
	CreatedAt  int64  `json:"created_at"`
}

func (LogRefundRecord) TableName() string { return "log_refund_records" }

// LogRefundRun 每次返费运行的审计记录，供管理端展示最近一次运行结果。
type LogRefundRun struct {
	Id            int    `json:"id" gorm:"primaryKey;autoIncrement"`
	TargetDate    string `json:"target_date" gorm:"type:varchar(10);not null;index:idx_log_refund_runs_target_date"`
	Trigger       string `json:"trigger" gorm:"type:varchar(16);not null"` // auto / manual
	LogCount      int    `json:"log_count"`
	UserCount     int    `json:"user_count"`
	RefundedQuota int    `json:"refunded_quota"`
	FailedCount   int    `json:"failed_count"`
	// Truncated 为 true 表示本次达到扫描上限、尚有候选日志未处理，需要继续执行。
	Truncated bool  `json:"truncated"`
	CreatedAt int64 `json:"created_at"`
}

func (LogRefundRun) TableName() string { return "log_refund_runs" }

// LogRefundPayload 系统任务载荷。Date 为北京时间自然日（YYYY-MM-DD），
// 为空时按「刚结束的那一天」处理。
type LogRefundPayload struct {
	Date    string `json:"date"`
	Trigger string `json:"trigger"`
}

// LogRefundResult 一次返费运行的结果汇总。
type LogRefundResult struct {
	TargetDate    string `json:"target_date"`
	LogCount      int    `json:"log_count"`
	UserCount     int    `json:"user_count"`
	RefundedQuota int    `json:"refunded_quota"`
	FailedCount   int    `json:"failed_count"`
	Truncated     bool   `json:"truncated"`
}

type logRefundCandidate struct {
	LogId int
	Quota int
}

func InitializeLogRefundConfig() error {
	return DB.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&LogRefundConfig{Id: LogRefundConfigID}).Error
}

func GetLogRefundConfig() (*LogRefundConfig, error) {
	config := LogRefundConfig{Id: LogRefundConfigID}
	err := DB.First(&config, LogRefundConfigID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &config, nil
	}
	return &config, err
}

func UpdateLogRefundEnabled(enabled bool) (*LogRefundConfig, error) {
	config := LogRefundConfig{Id: LogRefundConfigID, Enabled: enabled, UpdatedAt: time.Now().Unix()}
	if err := DB.Save(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// GetLatestLogRefundRun 返回最近一次返费运行记录；没有任何记录时返回 (nil, nil)。
func GetLatestLogRefundRun() (*LogRefundRun, error) {
	var run LogRefundRun
	err := DB.Order("id desc").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// GetLogRefundRunByDate 返回该日期最近一次返费运行记录；没有记录时返回 (nil, nil)。
func GetLogRefundRunByDate(targetDate string) (*LogRefundRun, error) {
	var run LogRefundRun
	err := DB.Where("target_date = ?", targetDate).Order("id desc").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// LogRefundDateProcessed 判断该日期是否已经有一次「完整完成」的返费运行：未被扫描上限
// 截断、且没有失败用户。管理端据此禁止对同一天重复执行「一键返费」。
// 若上轮被截断或存在失败用户，则返回 false，允许重跑把剩余部分补完。
func LogRefundDateProcessed(targetDate string) (bool, error) {
	var count int64
	err := DB.Model(&LogRefundRun{}).
		Where("target_date = ? AND truncated = ? AND failed_count = 0", targetDate, false).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// LogRefundTargetDate 返回北京时间下 now 当天的日期串。
func LogRefundTargetDate(now time.Time) string {
	return now.In(BeijingLocation()).Format("2006-01-02")
}

// LogRefundPreviousDate 返回北京时间下 now 的前一天日期串，即凌晨结算刚结束的那一天。
func LogRefundPreviousDate(now time.Time) string {
	return now.In(BeijingLocation()).AddDate(0, 0, -1).Format("2006-01-02")
}

// ValidateLogRefundTargetDate 校验日期串是否为合法的北京时间自然日。
func ValidateLogRefundTargetDate(date string) error {
	if _, err := time.ParseInLocation("2006-01-02", date, BeijingLocation()); err != nil {
		return ErrLogRefundInvalidDate
	}
	return nil
}

func logRefundDayRange(targetDate string) (int64, int64, error) {
	day, err := time.ParseInLocation("2006-01-02", targetDate, BeijingLocation())
	if err != nil {
		return 0, 0, ErrLogRefundInvalidDate
	}
	start := day.Unix()
	return start, start + 86400, nil
}

// isAbnormalLogCandidate 判定一条消费日志是否属于「异常返费」范围：必须确实扣了费
// （quota > 0），并且满足下面任一条件：
//  1. 空返回：补全 token 为 0，钱花了但一点输出都没拿到；
//  2. 流状态异常：流以错误结束，且输入/输出 token 不完整（任一为 0）。
//
// 注意：流状态异常但输入、输出 token 都正常非 0 时，说明内容已经正常产出，不算白扣，
// 不参与返费。
func isAbnormalLogCandidate(log *Log) bool {
	if log == nil || log.Quota <= 0 {
		return false
	}
	// 条件 1：空返回
	if log.CompletionTokens <= 0 {
		return true
	}
	if !log.IsStream {
		return false
	}
	if gjson.Get(log.Other, "stream_status.status").String() != "error" {
		return false
	}
	// 条件 2：流状态异常，但输入、输出 token 都正常 —— 内容已经产出，不返费
	if log.PromptTokens > 0 && log.CompletionTokens > 0 {
		return false
	}
	return true
}

// logRefundAmountText 按站点「额度展示」设置渲染退款金额：USD/CNY/自定义货币走
// 对应符号与汇率，TOKENS 直接显示原始额度；规则与 logger.FormatQuota 一致，
// 只是这里保留两位小数，用于用户日志文案。
func logRefundAmountText(quota int) string {
	if quota <= 0 || common.QuotaPerUnit <= 0 {
		return "0"
	}
	usd := decimal.NewFromInt(int64(quota)).Div(decimal.NewFromFloat(common.QuotaPerUnit))
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeTokens:
		return strconv.Itoa(quota)
	case operation_setting.QuotaDisplayTypeCNY:
		cny := usd.Mul(decimal.NewFromFloat(operation_setting.USDExchangeRate))
		return "¥" + cny.StringFixed(2)
	case operation_setting.QuotaDisplayTypeCustom:
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		return symbol + usd.Mul(decimal.NewFromFloat(rate)).StringFixed(2)
	default:
		return "$" + usd.StringFixed(2)
	}
}

// RecordLogRefund 写一条用户可见的返费日志（type=refund）。quota 字段同时写入，
// 前端金额列会再按站点展示设置渲染一次。
func RecordLogRefund(userId int, content string, quota int, targetDate string, logCount int) {
	username, _ := GetUsernameById(userId, false)
	other := NewLogOther()
	other.SetPublic("refund_date", targetDate)
	other.SetPublic("refund_log_count", logCount)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeRefund,
		Content:   content,
		Quota:     quota,
		Other:     other.JSONString(),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record log refund: " + err.Error())
	}
}

// HasPendingLogRefundAutoRun 判断「刚结束的那一天」是否还需要自动返费。调度器用它
// 做闸门：自动跑完当天就停；若上一轮达到扫描上限（truncated）或存在失败用户，则
// 返回 true，调度器稍后会继续把剩余部分处理完。管理端手动触发的记录不参与判定。
func HasPendingLogRefundAutoRun(now time.Time) bool {
	target := LogRefundPreviousDate(now)
	var latest LogRefundRun
	err := DB.Where("target_date = ? AND trigger = ?", target, "auto").Order("id desc").First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	if err != nil {
		common.SysError("log refund scheduler: " + err.Error())
		return false
	}
	return latest.Truncated || latest.FailedCount > 0
}

func logRefundProcessedLogIds(targetDate string) (map[int]struct{}, error) {
	var ids []int
	if err := DB.Model(&LogRefundRecord{}).Where("refund_date = ?", targetDate).Pluck("log_id", &ids).Error; err != nil {
		return nil, err
	}
	processed := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		processed[id] = struct{}{}
	}
	return processed, nil
}

// collectLogRefundCandidates 分批扫描目标日期的消费日志，交给 Go 侧做精确判定，
// 避免在 SQL 里解析 JSON（三种数据库的 JSON 能力不一致）。
// 第二个返回值表示本次是否因达到扫描上限而被截断。
func collectLogRefundCandidates(targetDate string) (map[int][]logRefundCandidate, bool, error) {
	start, end, err := logRefundDayRange(targetDate)
	if err != nil {
		return nil, false, err
	}
	processed, err := logRefundProcessedLogIds(targetDate)
	if err != nil {
		return nil, false, err
	}

	perUser := make(map[int][]logRefundCandidate)
	scanned := 0
	truncated := false
	for offset := 0; ; offset += logRefundBatchSize {
		var logs []Log
		// 消费日志在 LOG_DB（可能与主库分离，也支持 ClickHouse），必须走 LOG_DB 查询。
		if err := LOG_DB.Model(&Log{}).
			Where("type = ? AND created_at >= ? AND created_at < ? AND quota > ? AND (completion_tokens <= ? OR is_stream = ?)",
				LogTypeConsume, start, end, 0, 0, true).
			Order("id asc").
			Limit(logRefundBatchSize).
			Offset(offset).
			Find(&logs).Error; err != nil {
			return nil, false, err
		}
		if len(logs) == 0 {
			break
		}
		scanned += len(logs)
		for i := range logs {
			log := &logs[i]
			if _, done := processed[log.Id]; done {
				continue
			}
			if !isAbnormalLogCandidate(log) {
				continue
			}
			perUser[log.UserId] = append(perUser[log.UserId], logRefundCandidate{LogId: log.Id, Quota: log.Quota})
		}
		if len(logs) < logRefundBatchSize {
			break
		}
		if scanned >= maxLogRefundScanRows {
			truncated = true
			break
		}
	}
	return perUser, truncated, nil
}

// refundUserAbnormalLogs 在单个事务里为用户登记幂等记录并退还额度。
// 只有真正插入成功的记录才计入退款额，因此重复运行不会多退。
// 返回 (实际退款额度, 实际退款日志数, error)。
func refundUserAbnormalLogs(userId int, targetDate string, candidates []logRefundCandidate, now time.Time) (int, int, error) {
	refundedQuota := 0
	refundedCount := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, candidate := range candidates {
			record := LogRefundRecord{
				LogId:      candidate.LogId,
				UserId:     userId,
				RefundDate: targetDate,
				Quota:      candidate.Quota,
				CreatedAt:  now.Unix(),
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 1 {
				refundedQuota += candidate.Quota
				refundedCount++
			}
		}
		if refundedQuota <= 0 {
			return nil
		}
		// 条件更新保证余额不会超过钱包上限；不满足时整批回滚，宁可不退也不越界。
		result := tx.Model(&User{}).
			Where("id = ? AND quota <= ?", userId, common.MaxWalletQuota-refundedQuota).
			Update("quota", gorm.Expr("quota + ?", refundedQuota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errLogRefundWalletLimit
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return refundedQuota, refundedCount, nil
}

// RefundAbnormalConsumeLogs 执行一次异常返费：扫描 targetDate（北京时间自然日）内的
// 消费日志，按用户汇总退还额度，并为每个用户写一条汇总日志。
// trigger 仅用于审计记录（auto / manual）。
func RefundAbnormalConsumeLogs(targetDate string, now time.Time, trigger string) (*LogRefundResult, error) {
	if err := ValidateLogRefundTargetDate(targetDate); err != nil {
		return nil, err
	}
	perUser, truncated, err := collectLogRefundCandidates(targetDate)
	if err != nil {
		return nil, err
	}

	result := &LogRefundResult{TargetDate: targetDate, Truncated: truncated}
	for userId, candidates := range perUser {
		refundedQuota, refundedCount, err := refundUserAbnormalLogs(userId, targetDate, candidates, now)
		if err != nil {
			common.SysError(fmt.Sprintf("log refund failed: user=%d date=%s err=%v", userId, targetDate, err))
			result.FailedCount++
			continue
		}
		if refundedQuota <= 0 {
			continue
		}
		syncCreditUserQuotaCache(userId, refundedQuota, "log refund")
		RecordLogRefund(userId, "今日异常返费 "+logRefundAmountText(refundedQuota), refundedQuota, targetDate, refundedCount)
		result.LogCount += refundedCount
		result.UserCount++
		result.RefundedQuota += refundedQuota
	}

	if trigger == "" {
		trigger = "manual"
	}
	if err := DB.Create(&LogRefundRun{
		TargetDate:    result.TargetDate,
		Trigger:       trigger,
		LogCount:      result.LogCount,
		UserCount:     result.UserCount,
		RefundedQuota: result.RefundedQuota,
		FailedCount:   result.FailedCount,
		Truncated:     result.Truncated,
		CreatedAt:     now.Unix(),
	}).Error; err != nil {
		common.SysError("failed to record log refund run: " + err.Error())
	}
	return result, nil
}
