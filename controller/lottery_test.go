package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func lotteryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldLog := model.DB, model.LOG_DB
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldMaster := common.RedisEnabled, common.IsMasterNode
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.IsMasterNode = false, true
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "lottery.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	model.DB, model.LOG_DB = db, db
	// 代表已有用户表升级：数据先写入，再增加抽奖表，重复迁移不得丢失旧数据。
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, db.Create(&model.User{Id: 99, Username: "existing", AffCode: "existing", Quota: 123, Status: 1}).Error)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.LotteryConfig{}, &model.LotteryRound{}, &model.LotteryEntry{}, &model.LotteryWinner{}))
		require.NoError(t, model.InitializeLotteryConfig())
	}
	var existing model.User
	require.NoError(t, db.First(&existing, 99).Error)
	assert.Equal(t, 123, existing.Quota)
	require.NoError(t, authz.Init(db))
	var version string
	require.NoError(t, db.Raw("select sqlite_version()").Scan(&version).Error)
	t.Log("SQLite", version)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMain, oldLogType)
		common.RedisEnabled, common.IsMasterNode = oldRedis, oldMaster
		require.NoError(t, sqlDB.Close())
	})
	return db
}
func lotteryUser(t *testing.T, db *gorm.DB, id int, name string) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{Id: id, Username: name, Password: "not-a-secret", Email: "private@example.com", AffCode: fmt.Sprint(id), Quota: 10, Status: common.UserStatusEnabled}).Error)
}
func lotteryTime() time.Time { return time.Date(2026, 1, 1, 23, 59, 59, 0, model.BeijingLocation()) }

func TestLotteryEntryLimitsSnapshotAndDisabled(t *testing.T) {
	db := lotteryTestDB(t)
	now := lotteryTime()
	for i := 1; i <= 3; i++ {
		lotteryUser(t, db, i, fmt.Sprintf("user%d", i))
	}
	_, err := model.JoinLottery(1, now)
	require.ErrorIs(t, err, model.ErrLotteryDisabled)
	_, err = model.UpdateLotteryConfig(true, 2, 1, 17, 0)
	require.NoError(t, err)
	round, err := model.JoinLottery(1, now)
	require.NoError(t, err)
	assert.Equal(t, 17, round.RewardQuota)
	_, err = model.JoinLottery(1, now)
	assert.ErrorIs(t, err, model.ErrLotteryAlreadyJoined)
	_, err = model.UpdateLotteryConfig(true, 10, 3, 50, 0)
	require.NoError(t, err)
	round, err = model.JoinLottery(2, now)
	require.NoError(t, err)
	assert.Equal(t, 17, round.RewardQuota)
	assert.Equal(t, 2, round.ParticipantLimit)
	_, err = model.JoinLottery(3, now)
	assert.ErrorIs(t, err, model.ErrLotteryFull)
	_, err = model.UpdateLotteryConfig(false, 10, 3, 50, 0)
	require.NoError(t, err)
	require.NoError(t, model.SettleDueLotteryRounds(now))
	var count int64
	require.NoError(t, db.Model(&model.LotteryWinner{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, model.SettleDueLotteryRounds(now.Add(time.Second)))
	var winners []model.LotteryWinner
	require.NoError(t, db.Find(&winners).Error)
	require.Len(t, winners, 1)
	assert.Equal(t, 17, winners[0].RewardQuota)
	_, err = model.UpdateLotteryConfig(true, 10, 3, 50, 0)
	require.NoError(t, err)
	_, err = model.JoinLottery(winners[0].UserID, now.Add(time.Second))
	assert.ErrorIs(t, err, model.ErrLotteryHistoricalWinner)
	round, err = model.JoinLottery(3, now.Add(time.Second))
	require.NoError(t, err)
	assert.Equal(t, 50, round.RewardQuota)
	var user model.User
	require.NoError(t, db.First(&user, 3).Error)
	assert.Equal(t, 10, user.Quota, "entry fee is zero")
}

func TestLotteryEligibleOnlyShortfallEmptyAndCatchup(t *testing.T) {
	db := lotteryTestDB(t)
	now := lotteryTime()
	_, err := model.UpdateLotteryConfig(true, 5, 5, 11, 0)
	require.NoError(t, err)
	for i := 1; i <= 3; i++ {
		lotteryUser(t, db, i, fmt.Sprintf("user%d", i))
		_, err := model.JoinLottery(i, now)
		require.NoError(t, err)
	}
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 2).Update("status", common.UserStatusDisabled).Error)
	require.NoError(t, db.Delete(&model.User{}, 3).Error)
	// 新日报名前补结算；上一日中奖人不能趁启动延迟进入新一日。
	_, err = model.JoinLottery(1, now.Add(time.Second))
	assert.ErrorIs(t, err, model.ErrLotteryHistoricalWinner)
	var winners []model.LotteryWinner
	require.NoError(t, db.Find(&winners).Error)
	require.Len(t, winners, 1)
	assert.Equal(t, 1, winners[0].UserID)
	require.NoError(t, db.Create(&model.LotteryRound{DrawDate: "2025-12-31", ParticipantLimit: 5, WinnerLimit: 5, RewardQuota: 11, Status: model.LotteryPending}).Error)
	require.NoError(t, model.SettleDueLotteryRounds(now.Add(time.Second)))
	var empty model.LotteryRound
	require.NoError(t, db.Where("draw_date = ?", "2025-12-31").First(&empty).Error)
	assert.Equal(t, model.LotterySettled, empty.Status)
	_, err = model.JoinLottery(2, now.Add(time.Second))
	assert.ErrorIs(t, err, model.ErrLotteryUserUnavailable)
}

func TestLotteryConcurrentSettlementCreditsExactlyOnce(t *testing.T) {
	db := lotteryTestDB(t)
	now := lotteryTime()
	_, err := model.UpdateLotteryConfig(true, 4, 2, 7, 0)
	require.NoError(t, err)
	for i := 1; i <= 4; i++ {
		lotteryUser(t, db, i, fmt.Sprintf("user%d", i))
		_, err := model.JoinLottery(i, now)
		require.NoError(t, err)
	}
	start := make(chan struct{})
	results := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { <-start; results <- model.SettleDueLotteryRounds(now.Add(time.Second)) })
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.NoError(t, model.SettleDueLotteryRounds(now.Add(48*time.Hour)))
	var winners []model.LotteryWinner
	require.NoError(t, db.Find(&winners).Error)
	require.Len(t, winners, 2)
	assert.NotEqual(t, winners[0].UserID, winners[1].UserID)
	var users []model.User
	require.NoError(t, db.Where("id < ?", 99).Find(&users).Error)
	sum := 0
	for _, user := range users {
		sum += user.Quota
	}
	assert.Equal(t, 54, sum)
	assert.Error(t, db.Create(&model.LotteryWinner{DrawDate: "2026-01-05", UserID: winners[0].UserID, RewardQuota: 7}).Error)
}

func TestLotteryConcurrentEntryCannotOverfill(t *testing.T) {
	db := lotteryTestDB(t)
	now := lotteryTime()
	_, err := model.UpdateLotteryConfig(true, 1, 1, 7, 1)
	require.NoError(t, err)
	lotteryUser(t, db, 1, "first")
	lotteryUser(t, db, 2, "second")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []int{1, 2} {
		wg.Go(func() { <-start; _, err := model.JoinLottery(id, now); results <- err })
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			assert.ErrorIs(t, err, model.ErrLotteryFull)
		}
	}
	assert.Equal(t, 1, successes)
	// 只有报名成功的用户被扣报名费，失败者分文不动。
	var entrants []model.User
	require.NoError(t, db.Where("id in ?", []int{1, 2}).Find(&entrants).Error)
	quotaSum := 0
	for _, entrant := range entrants {
		quotaSum += entrant.Quota
	}
	assert.Equal(t, 19, quotaSum, "only the successful entry pays the fee")
}

func TestLotteryCreditFailureRollsBackWinnerAndRound(t *testing.T) {
	db := lotteryTestDB(t)
	now := lotteryTime()
	lotteryUser(t, db, 1, "rollback")
	_, err := model.UpdateLotteryConfig(true, 1, 1, 17, 0)
	require.NoError(t, err)
	_, err = model.JoinLottery(1, now)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("quota", common.MaxWalletQuota).Error)
	assert.ErrorIs(t, model.SettleDueLotteryRounds(now.Add(time.Second)), model.ErrWalletQuotaLimitExceeded)
	var count int64
	require.NoError(t, db.Model(&model.LotteryWinner{}).Count(&count).Error)
	assert.Zero(t, count)
	var round model.LotteryRound
	require.NoError(t, db.First(&round).Error)
	assert.Equal(t, model.LotteryPending, round.Status)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("quota", 10).Error)
	require.NoError(t, model.SettleDueLotteryRounds(now.Add(time.Second)))
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 27, user.Quota)
}

func lotteryRequest(t *testing.T, role int, method, path, body string, handler gin.HandlerFunc, permission *authz.Permission) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", role); c.Next() })
	if permission != nil {
		router.Use(middleware.RequirePermission(*permission))
	}
	router.Handle(method, strings.Split(path, "?")[0], handler)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}
func TestLotteryPublicPrivacyAndAdminPermission(t *testing.T) {
	db := lotteryTestDB(t)
	today := time.Now()
	lotteryUser(t, db, 1, "秘密完整用户名")
	_, err := model.UpdateLotteryConfig(true, 5, 5, 9, 0)
	require.NoError(t, err)
	// 伪造 body 不会替换会话用户身份。
	result := lotteryRequest(t, common.RoleCommonUser, http.MethodPost, "/lottery/join", `{"user_id":99,"username":"existing"}`, JoinLottery, nil)
	require.Equal(t, 200, result.Code)
	for _, role := range []int{common.RoleCommonUser, common.RoleRootUser} {
		result := lotteryRequest(t, role, http.MethodGet, "/lottery", "", GetLotteryStatus, nil)
		require.Equal(t, 200, result.Code)
		assert.Contains(t, result.Body.String(), "秘***")
		assert.NotContains(t, result.Body.String(), "秘密完整用户名")
		assert.NotContains(t, result.Body.String(), "private@example.com")
		assert.NotContains(t, result.Body.String(), "user_id")
	}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
		result := lotteryRequest(t, role, http.MethodGet, "/lottery/admin", "", GetLotteryAdmin, &authz.LotteryRead)
		if role == common.RoleCommonUser {
			assert.Equal(t, 403, result.Code)
		} else {
			assert.Equal(t, 200, result.Code)
			assert.Contains(t, result.Body.String(), "秘密完整用户名")
		}
	}
	require.NoError(t, authz.SetUserPermissions(1, authz.PermissionsMap{"lottery": {"read": false, "manage": false}}))
	denied := lotteryRequest(t, common.RoleAdminUser, http.MethodPut, "/lottery/admin", `{"enabled":true,"daily_participant_limit":1,"daily_winner_limit":1,"reward_quota":100,"entry_fee":0}`, UpdateLotteryAdmin, &authz.LotteryManage)
	assert.Equal(t, 403, denied.Code)
	for i, name := range []string{"甲", "甲乙", "😀乙"} {
		lotteryUser(t, db, i+2, name)
		_, err := model.JoinLottery(i+2, today)
		require.NoError(t, err)
	}
	status, err := model.GetLotteryPublicStatus(1, today, 1, 1)
	require.NoError(t, err)
	for _, row := range status.Participants[1:] {
		assert.Equal(t, "***", row.Username)
	}
}

func TestLotteryConfigValidationAndReadonlyStatus(t *testing.T) {
	db := lotteryTestDB(t)
	for _, config := range []model.LotteryConfig{{Enabled: true}, {Enabled: true, DailyParticipantLimit: 1, DailyWinnerLimit: 2, RewardQuota: 1}, {Enabled: true, DailyParticipantLimit: 1, DailyWinnerLimit: 1, RewardQuota: common.MaxWalletQuota + 1}, {Enabled: true, DailyParticipantLimit: 1, DailyWinnerLimit: 1, RewardQuota: 1, EntryFee: common.MaxWalletQuota + 1}, {DailyParticipantLimit: -1}, {EntryFee: -1}} {
		assert.ErrorIs(t, model.ValidateLotteryConfig(config), model.ErrLotteryInvalidConfig)
	}
	for _, reward := range []string{"1.5", "9007199254740992", "-1"} {
		result := lotteryRequest(t, common.RoleRootUser, http.MethodPut, "/lottery/admin", fmt.Sprintf(`{"enabled":true,"daily_participant_limit":1,"daily_winner_limit":1,"reward_quota":%s,"entry_fee":0}`, reward), UpdateLotteryAdmin, &authz.LotteryManage)
		assert.Equal(t, 400, result.Code)
	}
	_, err := model.GetLotteryPublicStatus(1, time.Now(), 1, 1)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&model.LotteryRound{}).Count(&count).Error)
	assert.Zero(t, count)
	assert.Equal(t, "2026-01-02", model.LotteryDrawDate(time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC)))
	assert.False(t, errors.Is(model.ErrLotteryInvalidConfig, model.ErrLotteryDisabled))
}

func TestLotteryEntryFeeChargedWithRoundSnapshot(t *testing.T) {
	db := lotteryTestDB(t)
	now := lotteryTime()
	lotteryUser(t, db, 1, "payer")
	lotteryUser(t, db, 2, "later")
	_, err := model.UpdateLotteryConfig(true, 10, 3, 17, 3)
	require.NoError(t, err)
	round, err := model.JoinLottery(1, now)
	require.NoError(t, err)
	assert.Equal(t, 3, round.EntryFee)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 7, user.Quota, "entry fee is deducted from the balance")
	// 中途改费用只影响尚未开始的轮次，当天快照保持不变。
	_, err = model.UpdateLotteryConfig(true, 10, 3, 17, 9)
	require.NoError(t, err)
	round, err = model.JoinLottery(2, now)
	require.NoError(t, err)
	assert.Equal(t, 3, round.EntryFee)
	var later model.User
	require.NoError(t, db.First(&later, 2).Error)
	assert.Equal(t, 7, later.Quota)
}

func TestLotteryEntryFeeInsufficientQuotaRejectsEntry(t *testing.T) {
	db := lotteryTestDB(t)
	now := lotteryTime()
	lotteryUser(t, db, 1, "poor")
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("quota", 2).Error)
	_, err := model.UpdateLotteryConfig(true, 10, 3, 17, 5)
	require.NoError(t, err)
	_, err = model.JoinLottery(1, now)
	assert.ErrorIs(t, err, model.ErrLotteryInsufficientQuota)
	var entries, rounds int64
	require.NoError(t, db.Model(&model.LotteryEntry{}).Count(&entries).Error)
	require.NoError(t, db.Model(&model.LotteryRound{}).Count(&rounds).Error)
	assert.Zero(t, entries, "failed entry must not be recorded")
	assert.Zero(t, rounds, "failed entry must roll back the round too")
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 2, user.Quota, "failed entry must not deduct the balance")
}
