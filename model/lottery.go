package model

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	LotteryConfigID        = 1
	LotteryPending         = "pending"
	LotterySettled         = "settled"
	LotteryMaxParticipants = 1_000_000
	LotteryMaxWinners      = 100_000
)

var (
	ErrLotteryDisabled          = errors.New("lottery is disabled")
	ErrLotteryAlreadyJoined     = errors.New("already joined today's lottery")
	ErrLotteryFull              = errors.New("today's lottery is full")
	ErrLotteryHistoricalWinner  = errors.New("historical winners cannot join again")
	ErrLotteryInvalidConfig     = errors.New("invalid lottery configuration")
	ErrLotteryUserUnavailable   = errors.New("user is unavailable")
	ErrLotteryInsufficientQuota = errors.New("点数余额不足，无法报名抽奖")
)

type LotteryConfig struct {
	ID                    int  `json:"id" gorm:"primaryKey"`
	Enabled               bool `json:"enabled"`
	DailyParticipantLimit int  `json:"daily_participant_limit"`
	DailyWinnerLimit      int  `json:"daily_winner_limit"`
	RewardQuota           int  `json:"reward_quota"`
	// EntryFee 为每次报名的原生额度点数消耗，0 表示免费报名。
	// default:0 保证旧库 ALTER TABLE 加列后不会留下 NULL。
	EntryFee  int   `json:"entry_fee" gorm:"not null;default:0"`
	UpdatedAt int64 `json:"updated_at"`
}

func (LotteryConfig) TableName() string { return "lottery_configs" }

type LotteryRound struct {
	ID               int    `json:"id" gorm:"primaryKey;autoIncrement"`
	DrawDate         string `json:"draw_date" gorm:"type:varchar(10);not null;uniqueIndex"`
	ParticipantLimit int    `json:"participant_limit" gorm:"not null"`
	WinnerLimit      int    `json:"winner_limit" gorm:"not null"`
	RewardQuota      int    `json:"reward_quota" gorm:"not null"`
	// EntryFee 是本轮报名费的快照，中途改配置不影响已开始的轮次。
	EntryFee  int    `json:"entry_fee" gorm:"not null;default:0"`
	Status    string `json:"status" gorm:"type:varchar(16);not null;index"`
	SettledAt int64  `json:"settled_at"`
	CreatedAt int64  `json:"created_at"`
}

func (LotteryRound) TableName() string { return "lottery_rounds" }

type LotteryEntry struct {
	ID               int    `json:"id" gorm:"primaryKey;autoIncrement"`
	DrawDate         string `json:"draw_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_lottery_entry_date_user"`
	UserID           int    `json:"user_id" gorm:"not null;uniqueIndex:idx_lottery_entry_date_user;index"`
	UsernameSnapshot string `json:"username_snapshot" gorm:"type:varchar(255);not null"`
	CreatedAt        int64  `json:"created_at"`
}

func (LotteryEntry) TableName() string { return "lottery_entries" }

type LotteryWinner struct {
	ID               int    `json:"id" gorm:"primaryKey;autoIncrement"`
	DrawDate         string `json:"draw_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_lottery_winner_date_user"`
	UserID           int    `json:"user_id" gorm:"not null;uniqueIndex:idx_lottery_winner_date_user;uniqueIndex:idx_lottery_winner_user"`
	UsernameSnapshot string `json:"username_snapshot" gorm:"type:varchar(255);not null"`
	RewardQuota      int    `json:"reward_quota" gorm:"not null"`
	CreditedAt       int64  `json:"credited_at"`
}

func (LotteryWinner) TableName() string { return "lottery_winners" }

// InitializeLotteryConfig 只插入关闭的空配置，不覆盖管理员配置。
func InitializeLotteryConfig() error {
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&LotteryConfig{ID: LotteryConfigID}).Error
}

func BeijingLocation() *time.Location      { return time.FixedZone("Asia/Shanghai", 8*60*60) }
func LotteryDrawDate(now time.Time) string { return now.In(BeijingLocation()).Format("2006-01-02") }
func GetLotteryConfig() (*LotteryConfig, error) {
	config := LotteryConfig{ID: LotteryConfigID}
	err := DB.First(&config, LotteryConfigID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &config, nil
	}
	return &config, err
}
func ValidateLotteryConfig(config LotteryConfig) error {
	if config.DailyParticipantLimit < 0 || config.DailyParticipantLimit > LotteryMaxParticipants || config.DailyWinnerLimit < 0 || config.DailyWinnerLimit > LotteryMaxWinners || config.RewardQuota < 0 || config.RewardQuota > common.MaxWalletQuota || config.EntryFee < 0 || config.EntryFee > common.MaxWalletQuota {
		return ErrLotteryInvalidConfig
	}
	// 关闭状态下的全零配置是合法的初始状态；报名费 0 表示免费，不算“已配置”。
	if !config.Enabled && config.DailyParticipantLimit == 0 && config.DailyWinnerLimit == 0 && config.RewardQuota == 0 && config.EntryFee == 0 {
		return nil
	}
	if config.DailyParticipantLimit <= 0 || config.DailyWinnerLimit <= 0 || config.RewardQuota <= 0 || config.DailyWinnerLimit > config.DailyParticipantLimit {
		return ErrLotteryInvalidConfig
	}
	return nil
}

// lotteryTransaction 的首条写语句先获取 SQLite 写锁，避免读后升级锁竞争；
// MySQL/Postgres 同样串行锁住配置行。唯一索引是最后一道防重入账保障。
func lotteryTransaction(fn func(*gorm.DB, *LotteryConfig) error) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&LotteryConfig{}).Where("id = ?", LotteryConfigID).UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return err
		}
		var config LotteryConfig
		if err := lockForUpdate(tx).First(&config, LotteryConfigID).Error; err != nil {
			return err
		}
		return fn(tx, &config)
	})
}
func UpdateLotteryConfig(enabled bool, participantLimit, winnerLimit, rewardQuota, entryFee int) (*LotteryConfig, error) {
	candidate := LotteryConfig{ID: LotteryConfigID, Enabled: enabled, DailyParticipantLimit: participantLimit, DailyWinnerLimit: winnerLimit, RewardQuota: rewardQuota, EntryFee: entryFee, UpdatedAt: time.Now().Unix()}
	if err := ValidateLotteryConfig(candidate); err != nil {
		return nil, err
	}
	err := lotteryTransaction(func(tx *gorm.DB, _ *LotteryConfig) error { return tx.Save(&candidate).Error })
	return &candidate, err
}

func JoinLottery(userID int, now time.Time) (*LotteryRound, error) {
	if userID <= 0 {
		return nil, ErrLotteryUserUnavailable
	}
	// 在报名事务之前补开奖；失败绝不让新一日先报名。
	if err := SettleDueLotteryRounds(now); err != nil {
		return nil, err
	}
	date := LotteryDrawDate(now)
	var joinedRound LotteryRound
	// chargedFee 只在事务内扣费成功时赋值，事务外据此同步缓存，回滚不会误扣。
	chargedFee := 0
	err := lotteryTransaction(func(tx *gorm.DB, config *LotteryConfig) error {
		var overdue int64
		if err := tx.Model(&LotteryRound{}).Where("status = ? AND draw_date < ?", LotteryPending, date).Count(&overdue).Error; err != nil {
			return err
		}
		if overdue > 0 {
			return errors.New("previous lottery settlement is pending")
		}
		if !config.Enabled {
			return ErrLotteryDisabled
		}
		if err := ValidateLotteryConfig(*config); err != nil {
			return err
		}
		var user User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return ErrLotteryUserUnavailable
		}
		if err := tx.Where("user_id = ?", userID).First(&LotteryWinner{}).Error; err == nil {
			return ErrLotteryHistoricalWinner
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Where("draw_date = ? AND user_id = ?", date, userID).First(&LotteryEntry{}).Error; err == nil {
			return ErrLotteryAlreadyJoined
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Where("draw_date = ?", date).First(&joinedRound).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			joinedRound = LotteryRound{DrawDate: date, ParticipantLimit: config.DailyParticipantLimit, WinnerLimit: config.DailyWinnerLimit, RewardQuota: config.RewardQuota, EntryFee: config.EntryFee, Status: LotteryPending, CreatedAt: now.Unix()}
			if err := tx.Create(&joinedRound).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if joinedRound.Status != LotteryPending {
			return ErrLotteryDisabled
		}
		var count int64
		if err := tx.Model(&LotteryEntry{}).Where("draw_date = ?", date).Count(&count).Error; err != nil {
			return err
		}
		if count >= int64(joinedRound.ParticipantLimit) {
			return ErrLotteryFull
		}
		// 报名费按本轮快照扣除；条件更新保证并发下不会把余额扣成负数。
		if joinedRound.EntryFee > 0 {
			result := tx.Model(&User{}).Where("id = ? AND status = ? AND quota >= ?", userID, common.UserStatusEnabled, joinedRound.EntryFee).Update("quota", gorm.Expr("quota - ?", joinedRound.EntryFee))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrLotteryInsufficientQuota
			}
			chargedFee = joinedRound.EntryFee
		}
		return tx.Create(&LotteryEntry{DrawDate: date, UserID: userID, UsernameSnapshot: user.Username, CreatedAt: now.Unix()}).Error
	})
	if err != nil {
		return nil, err
	}
	// 缓存同步必须放在事务提交之后：回滚时不会留下被多扣的缓存余额。
	if chargedFee > 0 {
		if err := cacheDecrUserQuota(userID, int64(chargedFee)); err != nil {
			common.SysLog("failed to sync lottery entry fee to user quota cache: " + err.Error())
		}
		RecordLog(userID, LogTypeSystem, fmt.Sprintf("Daily lottery %s entry: -%d quota points", date, chargedFee))
	}
	return &joinedRound, nil
}

// cryptoShuffle 使用无模偏差的密码学随机源，均匀无放回。
func cryptoShuffle(ids []int) error {
	for i := len(ids) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		j := int(n.Int64())
		ids[i], ids[j] = ids[j], ids[i]
	}
	return nil
}

// SettleDueLotteryRounds 每个事务只结算最早的过期轮次；关闭报名不取消承诺。
func SettleDueLotteryRounds(now time.Time) error {
	for {
		var settled bool
		var credited []LotteryWinner
		err := lotteryTransaction(func(tx *gorm.DB, _ *LotteryConfig) error {
			var round LotteryRound
			err := lockForUpdate(tx).Where("status = ? AND draw_date < ?", LotteryPending, LotteryDrawDate(now)).Order("draw_date").First(&round).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if round.RewardQuota <= 0 || round.RewardQuota > common.MaxWalletQuota || round.WinnerLimit <= 0 {
				return ErrLotteryInvalidConfig
			}
			var ids []int
			if err := tx.Model(&LotteryEntry{}).Where("draw_date = ?", round.DrawDate).Order("user_id").Pluck("user_id", &ids).Error; err != nil {
				return err
			}
			eligible := make([]int, 0, len(ids))
			names := make(map[int]string, len(ids))
			// 用户行按固定顺序上锁，封禁、删除与额度写入不能穿过本次结算。
			for _, id := range ids {
				var user User
				err := lockForUpdate(tx).First(&user, id).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				if user.Status != common.UserStatusEnabled {
					continue
				}
				var count int64
				if err := tx.Model(&LotteryWinner{}).Where("user_id = ?", id).Count(&count).Error; err != nil {
					return err
				}
				if count > 0 {
					continue
				}
				eligible = append(eligible, id)
				names[id] = user.Username
			}
			if err := cryptoShuffle(eligible); err != nil {
				return err
			}
			eligible = eligible[:min(len(eligible), round.WinnerLimit)]
			sort.Ints(eligible)
			for _, id := range eligible {
				winner := LotteryWinner{DrawDate: round.DrawDate, UserID: id, UsernameSnapshot: names[id], RewardQuota: round.RewardQuota, CreditedAt: now.Unix()}
				if err := tx.Create(&winner).Error; err != nil {
					return err
				}
				result := tx.Model(&User{}).Where("id = ? AND status = ? AND quota >= ? AND quota <= ?", id, common.UserStatusEnabled, -common.MaxWalletQuota, common.MaxWalletQuota-round.RewardQuota).Update("quota", gorm.Expr("quota + ?", round.RewardQuota))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return ErrWalletQuotaLimitExceeded
				}
				credited = append(credited, winner)
			}
			if err := tx.Model(&round).Updates(map[string]any{"status": LotterySettled, "settled_at": now.Unix()}).Error; err != nil {
				return err
			}
			settled = true
			return nil
		})
		if err != nil {
			return err
		}
		// 中奖记录本身是与余额同事务的不可重复入账凭据；日志是补充展示。
		for _, winner := range credited {
			syncCreditUserQuotaCache(winner.UserID, winner.RewardQuota, "lottery")
			RecordLog(winner.UserID, LogTypeSystem, fmt.Sprintf("Daily lottery %s: +%d quota points", winner.DrawDate, winner.RewardQuota))
		}
		if !settled {
			return nil
		}
	}
}
