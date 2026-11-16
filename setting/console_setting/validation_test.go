package console_setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidateAnnouncementsPopup 守住登录弹窗开关的校验契约：
// 旧数据必须继续通过，类型错误必须拒绝，弹窗数量必须有上限。
func TestValidateAnnouncementsPopup(t *testing.T) {
	t.Run("popup true is accepted", func(t *testing.T) {
		list := `[{"content":"hello","publishDate":"2026-01-01T00:00:00Z","popup":true}]`
		require.NoError(t, validateAnnouncements(list))
	})

	t.Run("legacy entries without popup still pass", func(t *testing.T) {
		list := `[{"content":"hello","publishDate":"2026-01-01T00:00:00Z"}]`
		require.NoError(t, validateAnnouncements(list))
	})

	t.Run("null popup is treated as unset", func(t *testing.T) {
		list := `[{"content":"hello","publishDate":"2026-01-01T00:00:00Z","popup":null}]`
		require.NoError(t, validateAnnouncements(list))
	})

	t.Run("non-boolean popup is rejected", func(t *testing.T) {
		list := `[{"content":"hello","publishDate":"2026-01-01T00:00:00Z","popup":"true"}]`
		require.Error(t, validateAnnouncements(list))
	})

	t.Run("popup count is capped", func(t *testing.T) {
		items := make([]string, 0, 6)
		for range 6 {
			items = append(items, `{"content":"hello","publishDate":"2026-01-01T00:00:00Z","popup":true}`)
		}
		require.Error(t, validateAnnouncements("["+strings.Join(items, ",")+"]"))
	})

	t.Run("non-popup entries do not count toward the cap", func(t *testing.T) {
		items := make([]string, 0, 101)
		for range 6 {
			items = append(items, `{"content":"hello","publishDate":"2026-01-01T00:00:00Z","popup":false}`)
		}
		require.NoError(t, validateAnnouncements("["+strings.Join(items, ",")+"]"))
	})
}
