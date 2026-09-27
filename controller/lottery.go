package controller

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type lotteryConfigRequest struct {
	Enabled               bool    `json:"enabled"`
	DailyParticipantLimit int     `json:"daily_participant_limit"`
	DailyWinnerLimit      int     `json:"daily_winner_limit"`
	RewardQuota           float64 `json:"reward_quota"`
	EntryFee              float64 `json:"entry_fee"`
}

// lotteryBalanceToQuota converts the admin-entered balance display amount into
// the database's native quota units. The frontend uses the same currency
// configuration; the server repeats the conversion so the client cannot set a
// different effective reward or fee.
func lotteryBalanceToQuota(amount float64) (int, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return 0, model.ErrLotteryInvalidConfig
	}
	balance := decimal.NewFromFloat(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		if common.QuotaPerUnit <= 0 {
			return 0, model.ErrLotteryInvalidConfig
		}
		balance = balance.Div(decimal.NewFromFloat(common.QuotaPerUnit))
	} else {
		rate := operation_setting.GetUsdToCurrencyRate(operation_setting.USDExchangeRate)
		if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return 0, model.ErrLotteryInvalidConfig
		}
		balance = balance.Div(decimal.NewFromFloat(rate))
	}
	quota, err := common.WalletQuotaFromDecimalStrict(
		balance.Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
	)
	if err != nil {
		return 0, model.ErrLotteryInvalidConfig
	}
	return quota, nil
}

func lotteryError(c *gin.Context, err error) {
	message := "Lottery operation failed. Please try again."
	status := http.StatusInternalServerError
	for _, known := range []error{model.ErrLotteryDisabled, model.ErrLotteryUserUnavailable, model.ErrLotteryInvalidConfig, model.ErrLotteryAlreadyJoined, model.ErrLotteryFull, model.ErrLotteryHistoricalWinner, model.ErrLotteryInsufficientQuota} {
		if errors.Is(err, known) {
			status = http.StatusBadRequest
			message = known.Error()
			break
		}
	}
	if status == http.StatusInternalServerError {
		common.SysError("lottery operation failed: " + err.Error())
	}
	c.JSON(status, gin.H{"success": false, "message": message})
}

func lotteryPages(c *gin.Context) (int, int, bool) {
	participantPage, err1 := strconv.Atoi(c.DefaultQuery("participant_page", "1"))
	winnerPage, err2 := strconv.Atoi(c.DefaultQuery("winner_page", "1"))
	if err1 != nil || err2 != nil || participantPage < 1 || winnerPage < 1 || participantPage > 1000000 || winnerPage > 1000000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid lottery page"})
		return 0, 0, false
	}
	return participantPage, winnerPage, true
}

// GetLotteryStatus 即使管理员访问也只返回后端脱敏 DTO。
func GetLotteryStatus(c *gin.Context) {
	p, w, ok := lotteryPages(c)
	if !ok {
		return
	}
	status, err := model.GetLotteryPublicStatus(c.GetInt("id"), time.Now(), p, w)
	if err != nil {
		lotteryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

// JoinLottery registers the authenticated session user; no username or user id is accepted.
func JoinLottery(c *gin.Context) {
	round, err := model.JoinLottery(c.GetInt("id"), time.Now())
	if err != nil {
		lotteryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "报名成功", "data": gin.H{"draw_date": round.DrawDate}})
}

func GetLotteryAdmin(c *gin.Context) {
	p, w, ok := lotteryPages(c)
	if !ok {
		return
	}
	data, err := model.GetLotteryAdminStatus(c.Query("date"), p, w)
	if err != nil {
		lotteryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func UpdateLotteryAdmin(c *gin.Context) {
	var request lotteryConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "抽奖配置格式无效"})
		return
	}
	rewardQuota, err := lotteryBalanceToQuota(request.RewardQuota)
	if err != nil {
		lotteryError(c, err)
		return
	}
	entryFee, err := lotteryBalanceToQuota(request.EntryFee)
	if err != nil {
		lotteryError(c, err)
		return
	}
	config, err := model.UpdateLotteryConfig(request.Enabled, request.DailyParticipantLimit, request.DailyWinnerLimit, rewardQuota, entryFee)
	if err != nil {
		lotteryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "抽奖配置已更新，新配置从下一轮报名开始生效", "data": config})
}
