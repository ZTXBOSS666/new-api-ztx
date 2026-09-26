import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState } from "@/components/error-state";
import { LoadingState } from "@/components/loading-state";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { hasPermission } from "@/lib/admin-permissions";
import { handleServerError } from "@/lib/handle-server-error";
import { useAuthStore } from "@/stores/auth-store";
import { getLotteryAdmin, saveLottery, type LotteryConfig } from "./api";
import { LotteryPeople, LotteryRules } from "./LotteryPeople";
import { lotteryConfigSchema } from "./lib/schema";

function LotteryConfigForm(props: {
  config: LotteryConfig;
  canManage: boolean;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const form = useForm<LotteryConfig>({
    resolver: zodResolver(lotteryConfigSchema),
    defaultValues: props.config,
  });
  const [pending, setPending] = useState<LotteryConfig | null>(null);
  const save = useMutation({
    mutationFn: saveLottery,
    onSuccess: (config) => {
      form.reset(config);
      setPending(null);
      toast.success(t("Lottery settings saved"));
      void queryClient.invalidateQueries({ queryKey: ["lottery"] });
    },
    onError: (error) => handleServerError(error),
  });
  const fields = [
    {
      name: "daily_participant_limit" as const,
      label: t("Daily participant limit"),
      max: 1000000,
    },
    {
      name: "daily_winner_limit" as const,
      label: t("Daily winners"),
      max: 100000,
    },
    {
      name: "reward_quota" as const,
      label: t("Reward per winner (native quota points)"),
      max: Number.MAX_SAFE_INTEGER,
    },
    {
      name: "entry_fee" as const,
      label: t("Entry fee per participant (native quota points)"),
      max: Number.MAX_SAFE_INTEGER,
    },
  ];
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("Lottery settings")}</CardTitle>
      </CardHeader>
      <CardContent>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(setPending)} className="space-y-4">
            <LotteryRules />
            <fieldset
              disabled={!props.canManage || save.isPending}
              className="grid gap-4 sm:grid-cols-4"
            >
              {fields.map((item) => (
                <FormField
                  key={item.name}
                  control={form.control}
                  name={item.name}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{item.label}</FormLabel>
                      <FormControl>
                        <Input
                          type="number"
                          min={0}
                          max={item.max}
                          step={1}
                          {...field}
                          onChange={(event) =>
                            field.onChange(
                              event.target.value === ""
                                ? Number.NaN
                                : Number(event.target.value),
                            )
                          }
                          value={Number.isNaN(field.value) ? "" : field.value}
                        />
                      </FormControl>
                      {form.formState.errors[item.name] && (
                        <p role="alert" className="text-destructive text-sm">
                          {t(
                            "Use whole numbers within the allowed limits; winners cannot exceed participants.",
                          )}
                        </p>
                      )}
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ))}
              <FormField
                control={form.control}
                name="enabled"
                render={({ field }) => (
                  <FormItem className="flex items-center gap-3 sm:col-span-4">
                    <FormLabel>{t("Enable lottery entries")}</FormLabel>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </fieldset>
            <p className="text-muted-foreground text-sm">
              {t(
                "Participant limit: 1–1,000,000. Winner limit: 1–100,000. Reward: 1–9,007,199,254,740,991 quota points. Entry fee: 0–9,007,199,254,740,991 quota points (0 means free entry, charged from the participant's balance). No default reward is configured.",
              )}
            </p>
            <Button type="submit" disabled={!props.canManage || save.isPending}>
              {t("Save lottery settings")}
            </Button>
            {save.isError && (
              <p role="alert" className="text-destructive">
                {t("Failed to save lottery settings")}
              </p>
            )}
          </form>
        </Form>
        <ConfirmDialog
          open={pending !== null}
          onOpenChange={(open) => {
            if (!open) setPending(null);
          }}
          title={t("Confirm lottery settings")}
          desc={t(
            "Only unstarted rounds use new limits and rewards. Disabling entries still settles existing rounds.",
          )}
          confirmText={t("Confirm save")}
          isLoading={save.isPending}
          handleConfirm={() => {
            if (pending) save.mutate(pending);
          }}
        />
      </CardContent>
    </Card>
  );
}

export function LotteryAdminPanel() {
  const { t } = useTranslation();
  const user = useAuthStore((state) => state.auth.user);
  const allowed = hasPermission(user, "lottery", "read");
  const [date, setDate] = useState("");
  const [participantPage, setParticipantPage] = useState(1);
  const [winnerPage, setWinnerPage] = useState(1);
  const query = useQuery({
    queryKey: ["lottery", "admin", date, participantPage, winnerPage],
    queryFn: () => getLotteryAdmin(date, participantPage, winnerPage),
    enabled: allowed,
  });
  if (!allowed) {
    return (
      <ErrorState title={t("Lottery administration permission required")} />
    );
  }
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
  return (
    <div className="space-y-4">
      <LotteryConfigForm
        config={data.config}
        canManage={hasPermission(user, "lottery", "manage")}
      />
      <div className="flex flex-wrap items-center gap-3">
        <Label htmlFor="lottery-admin-date">
          {t("Participant date (Beijing)")}
        </Label>
        <Input
          className="w-auto"
          id="lottery-admin-date"
          type="date"
          value={date}
          onChange={(event) => {
            setDate(event.target.value);
            setParticipantPage(1);
          }}
        />
        <Button
          variant="outline"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t("Refresh")}
        </Button>
      </div>
      <p className="text-muted-foreground text-sm">
        {t(
          "Authorized administration: usernames are shown in full. Winner history includes all dates.",
        )}
      </p>
      {data.round && (
        <p>
          {t(
            "Round snapshot: {{date}}, {{participants}} participants, {{winners}} winners, {{reward}} quota points each, entry fee {{fee}}",
            {
              date: data.round.draw_date,
              participants: data.round.participant_limit,
              winners: data.round.winner_limit,
              reward: data.round.reward_quota,
              fee: data.round.entry_fee,
            },
          )}
        </p>
      )}
      <div className="grid gap-4 xl:grid-cols-2">
        <LotteryPeople
          title={t("Participants (full names)")}
          rows={data.participants}
          total={data.participants_total}
          page={participantPage}
          onPage={setParticipantPage}
        />
        <LotteryPeople
          title={t("Winner history (full names)")}
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
