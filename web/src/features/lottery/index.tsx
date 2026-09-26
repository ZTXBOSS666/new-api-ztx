/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from "react-i18next";

import { SectionPageLayout } from "@/components/layout";

import { LotteryAdminPanel } from "./LotteryAdminPanel";
import { LotteryPanel } from "./LotteryPanel";

/**
 * 用户端每日抽奖页。
 *
 * 独立路由 /lottery：不再挂到数据看板的 section 体系下，
 * 因此不会和数据看板的“模型调用分析”“分流”等标签混在一起。
 */
export function Lottery() {
  const { t } = useTranslation();
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t("Daily lottery")}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <LotteryPanel />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  );
}

/**
 * 管理端抽奖页，独立路由 /lottery-admin，同样脱离数据看板标签。
 */
export function LotteryAdmin() {
  const { t } = useTranslation();
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t("Manage lottery")}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <LotteryAdminPanel />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  );
}
