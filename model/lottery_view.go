package model

import (
	"errors"
	"gorm.io/gorm"
	"time"
)

const LotteryPageSize = 20

type LotteryPerson struct {
	ID            int     `json:"id"`
	Username      string  `json:"username"`
	DrawDate      string  `json:"draw_date"`
	JoinedAt      int64   `json:"joined_at"`
	RewardBalance float64 `json:"reward_balance"`
	IsSelf        bool    `json:"is_self"`
}
type LotteryLists struct {
	Participants      []LotteryPerson `json:"participants"`
	Winners           []LotteryPerson `json:"winners"`
	ParticipantsTotal int64           `json:"participants_total"`
	WinnersTotal      int64           `json:"winners_total"`
}
type LotteryPublicStatus struct {
	LotteryLists
	Enabled          bool    `json:"enabled"`
	DrawDate         string  `json:"draw_date"`
	ParticipantLimit int     `json:"participant_limit"`
	WinnerLimit      int     `json:"winner_limit"`
	RewardBalance    float64 `json:"reward_balance"`
	EntryFeeBalance  float64 `json:"entry_fee_balance"`
	ParticipantCount int64   `json:"participant_count"`
	Joined           bool    `json:"joined"`
	Settled          bool    `json:"settled"`
}
type LotteryAdminStatus struct {
	LotteryLists
	Config LotteryConfig `json:"config"`
	Round  *LotteryRound `json:"round"`
}

// 脱敏不依赖客户端角色，短名和单字姓名完全隐藏。
func lotteryMaskName(name string) string {
	runes := []rune(name)
	if len(runes) <= 2 {
		return "***"
	}
	return string(runes[0]) + "***"
}

func lotteryLists(date string, participantPage, winnerPage, selfID int, plaintext bool) (LotteryLists, error) {
	out := LotteryLists{Participants: []LotteryPerson{}, Winners: []LotteryPerson{}}
	if participantPage < 1 || participantPage > 1000000 || winnerPage < 1 || winnerPage > 1000000 {
		return out, errors.New("invalid lottery page")
	}
	participants := DB.Model(&LotteryEntry{}).Where("draw_date = ?", date)
	if err := participants.Count(&out.ParticipantsTotal).Error; err != nil {
		return out, err
	}
	if err := DB.Model(&LotteryWinner{}).Count(&out.WinnersTotal).Error; err != nil {
		return out, err
	}
	var entries []LotteryEntry
	if err := participants.Order("id").Offset((participantPage - 1) * LotteryPageSize).Limit(LotteryPageSize).Find(&entries).Error; err != nil {
		return out, err
	}
	for _, entry := range entries {
		name := lotteryMaskName(entry.UsernameSnapshot)
		if plaintext {
			name = entry.UsernameSnapshot
		}
		out.Participants = append(out.Participants, LotteryPerson{ID: entry.ID, Username: name, DrawDate: entry.DrawDate, JoinedAt: entry.CreatedAt, IsSelf: entry.UserID == selfID})
	}
	var winners []LotteryWinner
	if err := DB.Order("draw_date DESC, id DESC").Offset((winnerPage - 1) * LotteryPageSize).Limit(LotteryPageSize).Find(&winners).Error; err != nil {
		return out, err
	}
	for _, winner := range winners {
		name := lotteryMaskName(winner.UsernameSnapshot)
		if plaintext {
			name = winner.UsernameSnapshot
		}
		out.Winners = append(out.Winners, LotteryPerson{ID: winner.ID, Username: name, DrawDate: winner.DrawDate, JoinedAt: winner.CreditedAt, RewardBalance: lotteryBalanceFromQuota(winner.RewardQuota), IsSelf: winner.UserID == selfID})
	}
	return out, nil
}

func GetLotteryPublicStatus(userID int, now time.Time, participantPage, winnerPage int) (*LotteryPublicStatus, error) {
	config, err := GetLotteryConfig()
	if err != nil {
		return nil, err
	}
	date := LotteryDrawDate(now)
	rewardBalance, entryFeeBalance := lotteryEffectiveBalances(*config)
	out := &LotteryPublicStatus{Enabled: config.Enabled, DrawDate: date, ParticipantLimit: config.DailyParticipantLimit, WinnerLimit: config.DailyWinnerLimit, RewardBalance: rewardBalance, EntryFeeBalance: entryFeeBalance}
	var round LotteryRound
	err = DB.Where("draw_date = ?", date).First(&round).Error
	if err == nil {
		roundReward, roundFee := round.RewardBalance, round.EntryFeeBalance
		if roundReward == 0 && round.RewardQuota > 0 {
			roundReward = lotteryBalanceFromQuota(round.RewardQuota)
		}
		if roundFee == 0 && round.EntryFee > 0 {
			roundFee = lotteryBalanceFromQuota(round.EntryFee)
		}
		out.ParticipantLimit, out.WinnerLimit, out.RewardBalance, out.EntryFeeBalance, out.Settled = round.ParticipantLimit, round.WinnerLimit, roundReward, roundFee, round.Status == LotterySettled
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var count int64
	if err := DB.Model(&LotteryEntry{}).Where("draw_date = ? AND user_id = ?", date, userID).Count(&count).Error; err != nil {
		return nil, err
	}
	out.Joined = count > 0
	out.LotteryLists, err = lotteryLists(date, participantPage, winnerPage, userID, false)
	out.ParticipantCount = out.ParticipantsTotal
	return out, err
}
func GetLotteryAdminStatus(date string, participantPage, winnerPage int) (*LotteryAdminStatus, error) {
	config, err := GetLotteryConfig()
	if err != nil {
		return nil, err
	}
	if date == "" {
		date = LotteryDrawDate(time.Now())
	}
	if parsed, err := time.Parse("2006-01-02", date); err != nil || parsed.Format("2006-01-02") != date {
		return nil, errors.New("invalid lottery date")
	}
	out := &LotteryAdminStatus{Config: *config}
	var round LotteryRound
	err = DB.Where("draw_date = ?", date).First(&round).Error
	if err == nil {
		if round.RewardBalance == 0 && round.RewardQuota > 0 {
			round.RewardBalance = lotteryBalanceFromQuota(round.RewardQuota)
		}
		if round.EntryFeeBalance == 0 && round.EntryFee > 0 {
			round.EntryFeeBalance = lotteryBalanceFromQuota(round.EntryFee)
		}
		out.Round = &round
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	out.LotteryLists, err = lotteryLists(date, participantPage, winnerPage, 0, true)
	return out, err
}
