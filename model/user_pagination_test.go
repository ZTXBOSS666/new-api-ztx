package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertUsersForPaginationTest(t *testing.T, total int) {
	t.Helper()
	for id := 1; id <= total; id++ {
		user := &User{
			Id:          id,
			Username:    fmt.Sprintf("user%02d", id),
			Password:    "password123",
			DisplayName: fmt.Sprintf("User %02d", id),
			Email:       fmt.Sprintf("user%02d@example.com", id),
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AffCode:     fmt.Sprintf("aff%02d", id),
		}
		require.NoError(t, DB.Create(user).Error)
	}
}

func collectUserIDs(users []*User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	return ids
}

func TestGetAllUsersSortsBeforePagination(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 42)

	pageOne, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, collectUserIDs(pageOne))

	pageTwo, total, err := GetAllUsers(&common.PageInfo{Page: 2, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(pageTwo))

	pageThree, total, err := GetAllUsers(&common.PageInfo{Page: 3, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{41, 42}, collectUserIDs(pageThree))
}

func TestSearchUsersSortsBeforePagination(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 42)

	users, total, err := SearchUsers("user", "", nil, nil, 20, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(users))
}

// TestGetMemberQuotaSummaryCountsOnlyMembers 守住会员余额汇总的口径：
// 只算普通会员，禁用会员的余额仍然是平台负债要计入，管理员和软删除用户不能混进来。
func TestGetMemberQuotaSummaryCountsOnlyMembers(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&User{Id: 1, Username: "member1", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "m1", Quota: 100}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member2", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, Group: "default", AffCode: "m2", Quota: 250}).Error)
	require.NoError(t, DB.Create(&User{Id: 3, Username: "admin1", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "a1", Quota: 9999}).Error)

	deleted := &User{Id: 4, Username: "deleted1", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "d1", Quota: 777}
	require.NoError(t, DB.Create(deleted).Error)
	require.NoError(t, DB.Delete(deleted).Error)

	summary, err := GetMemberQuotaSummary()
	require.NoError(t, err)
	assert.Equal(t, int64(2), summary.MemberCount, "禁用会员也要计入，但管理员和软删除用户不能算")
	assert.Equal(t, int64(350), summary.MemberQuota)
}

// TestGetMemberQuotaSummarySplitsActiveAndInactive 守住活跃度口径：
// quota > 0 算正常使用，quota <= 0（含被扣成负数）算无效客户，两者之和必须等于总数。
func TestGetMemberQuotaSummarySplitsActiveAndInactive(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&User{Id: 1, Username: "rich", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "r1", Quota: 500}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "zero", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "z1", Quota: 0}).Error)
	require.NoError(t, DB.Create(&User{Id: 3, Username: "negative", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "n1", Quota: -42}).Error)
	require.NoError(t, DB.Create(&User{Id: 4, Username: "disabled_rich", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, Group: "default", AffCode: "d1", Quota: 88}).Error)

	summary, err := GetMemberQuotaSummary()
	require.NoError(t, err)
	assert.Equal(t, int64(4), summary.MemberCount)
	assert.Equal(t, int64(2), summary.ActiveCount, "只有余额为正的算正常使用（含禁用的有余额会员）")
	assert.Equal(t, int64(2), summary.InactiveCount, "余额为 0 和负数都算无效客户")
	assert.Equal(t, summary.MemberCount, summary.ActiveCount+summary.InactiveCount, "两类必须覆盖全部会员")
}

// TestSearchUsersWithBalanceFilter 守住余额筛选：active 只返回余额为正的会员，
// inactive 返回余额 <= 0 的会员，非法值不筛选。
func TestSearchUsersWithBalanceFilter(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "rich", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "r1", Quota: 500}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "zero", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "z1", Quota: 0}).Error)
	require.NoError(t, DB.Create(&User{Id: 3, Username: "negative", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "n1", Quota: -42}).Error)

active, activeTotal, err := SearchUsersWithBalance("", "", nil, nil, UserBalanceActive, 0, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(1), activeTotal)
	assert.Equal(t, []int{1}, collectUserIDs(active))

	inactive, inactiveTotal, err := SearchUsersWithBalance("", "", nil, nil, UserBalanceInactive, 0, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), inactiveTotal)
	assert.Equal(t, []int{2, 3}, collectUserIDs(inactive))

	// 非法值退化为不筛选，不能把列表变成空的。
	all, allTotal, err := SearchUsersWithBalance("", "", nil, nil, ParseUserBalanceFilter("garbage"), 0, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(3), allTotal)
	assert.Equal(t, []int{1, 2, 3}, collectUserIDs(all))
}
