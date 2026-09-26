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
import { useStatus } from "@/hooks/use-status";
import { cn } from "@/lib/utils";

/**
 * 只展示后端注入的版本号。
 *
 * 本项目已经关闭上游 GitHub 更新检测，所以这里不发任何网络请求；
 * 版本号缺失或为占位值时保持安静，不再回退成 “Unknown version”。
 */
export function SystemVersionLabel(props: { className?: string }) {
  const { status } = useStatus();
  const version = status?.version?.trim();
  if (!version || version === "v0.0.0" || version === "0.0.0") {
    return null;
  }
  return (
    <span
      className={cn(
        "text-muted-foreground max-w-32 truncate font-mono text-xs",
        props.className,
      )}
      title={version}
    >
      {version}
    </span>
  );
}
