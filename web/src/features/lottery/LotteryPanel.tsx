import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ErrorState } from "@/components/error-state";
import { LoadingState } from "@/components/loading-state";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { toIntlLocale } from "@/i18n/languages";
import { hasPermission } from "@/lib/admin-permissions";
import { formatNumber } from "@/lib/format";
import { handleServerError } from "@/lib/handle-server-error";
import { useAuthStore } from "@/stores/auth-store";
import { getLottery, joinLottery } from "./api";
import { LotteryPeople, LotteryRules } from "./LotteryPeople";

export function LotteryPanel() {
  const { t, i18n } = useTranslation();
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language);
  const [participantPage, setParticipantPage] = useState(1);
  const [winnerPage, setWinnerPage] = useState(1);
  const queryClient = useQueryClient();
  const user = useAuthStore((s) => s.auth.user);
  const query = useQuery({
    queryKey: ["lottery", "public", participantPage, winnerPage],
    queryFn: () => getLottery(participantPage, winnerPage),
  });
  const join = useMutation({
    mutationFn: joinLottery,
    onSuccess: () => {
      toast.success(t("Lottery entry confirmed"));
      void queryClient.invalidateQueries({ queryKey: ["lottery"] });
    },
    onError: (error) => {
      handleServerError(error);
      void queryClient.invalidateQueries({ queryKey: ["lottery"] });
    },
  });
  if (query.isPending) {
    return <LoadingState message={t("Loading lottery...")} />;
  }
  if (query.isError) {
    return (
      <ErrorState
        title={t("Failed to load lottery")}
        onRetry={() => void query.refetch()}
      />
    );
  }
  const data = query.data;
  let label = t("Join daily lottery");
  if (!data.enabled) {
    label = t("Lottery entries disabled");
  } else if (data.historical_winner) {
    label = t("Already won — no further entries");
  } else if (data.joined) {
    label = t("Already joined today");
  } else if (data.participant_count >= data.participant_limit) {
    label = t("Lottery is full");
  }
  const disabled =
    !data.enabled ||
    data.historical_winner ||
    data.joined ||
    data.settled ||
    data.participant_count >= data.participant_limit ||
    join.isPending;
  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>{t("Daily lottery")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <LotteryRules />
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-5">
            <div>
              <dt>{t("Lottery date (Beijing)")}</dt>
              <dd>{data.draw_date}</dd>
            </div>
            <div>
              <dt>{t("Participants")}</dt>
              <dd>
                {formatNumber(data.participant_count, locale)} /{" "}
                {formatNumber(data.participant_limit, locale)}
              </dd>
            </div>
            <div>
              <dt>{t("Daily winners")}</dt>
              <dd>{formatNumber(data.winner_limit, locale)}</dd>
            </div>
            <div>
              <dt>{t("Reward (quota points)")}</dt>
              <dd>{formatNumber(data.reward_quota, locale)}</dd>
            </div>
            <div>
              <dt>{t("Entry fee (quota points)")}</dt>
              <dd>{formatNumber(data.entry_fee, locale)}</dd>
            </div>
          </dl>
          <div className="flex flex-wrap gap-2">
            <Button disabled={disabled} onClick={() => join.mutate()}>
              {join.isPending ? t("Submitting...") : label}
            </Button>
            <Button
              variant="outline"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t("Refresh")}
            </Button>
            {hasPermission(user, "lottery", "read") && (
              <Button variant="outline" render={<Link to="/lottery-admin" />}>
                {t("Manage lottery")}
              </Button>
            )}
          </div>
          {join.isError && (
            <p role="alert" className="text-destructive">
              {t("Lottery entry failed. Refresh and try again.")}
            </p>
          )}
        </CardContent>
      </Card>
      <p className="text-muted-foreground text-sm">
        {t(
          "All usernames below are masked by the server, including when viewed by administrators.",
        )}
      </p>
      <div className="grid gap-4 xl:grid-cols-2">
        <LotteryPeople
          title={t("Today’s participants")}
          rows={data.participants}
          total={data.participants_total}
          page={participantPage}
          onPage={setParticipantPage}
        />
        <LotteryPeople
          title={t("Winner history")}
          rows={data.winners}
          total={data.winners_total}
          page={winnerPage}
          onPage={setWinnerPage}
          winners
        />
      </div>
    </div>
  );
}
