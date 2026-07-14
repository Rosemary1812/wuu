import {
  useCallback,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import type {
  ParticipantProfile,
  ParticipantSaveParams,
} from "../shared/protocol";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

const DEFAULT_PARTICIPANT_ARCHIVED_NOTICE_MS = 1_500;

export type ParticipantPanelState = {
  mode: "new" | "edit";
  participant?: ParticipantProfile;
  initialName?: string;
  loading: boolean;
  error?: string;
  saving?: boolean;
  feedbackSubmitting?: boolean;
  feedbackReply?: string;
  retiring?: boolean;
  archived?: boolean;
};

type ParticipantTeamTemplate = {
  version: number;
  participants: ParticipantSaveParams[];
};

export type ParticipantStateOptions = {
  initialized: boolean;
  setStatus: (status: string) => void;
  archivedNoticeMs?: number;
};

export type ParticipantStateController = {
  participants: ParticipantProfile[];
  setParticipants: Dispatch<SetStateAction<ParticipantProfile[]>>;
  participantPanel: ParticipantPanelState | undefined;
  setParticipantPanel: Dispatch<
    SetStateAction<ParticipantPanelState | undefined>
  >;
  refreshParticipants: () => Promise<ParticipantProfile[]>;
  handleParticipantDialogSave: (
    params: ParticipantSaveParams,
  ) => Promise<ParticipantProfile>;
  handleParticipantSave: (params: ParticipantSaveParams) => void;
  handleParticipantFeedback: (text: string) => void;
  handleParticipantRetire: (participantID: string) => void;
  exportParticipantTemplate: () => void;
  importParticipantTemplate: (file: File) => Promise<void>;
};

function replaceParticipantProfile(
  participants: ParticipantProfile[],
  next: ParticipantProfile,
): ParticipantProfile[] {
  const index = participants.findIndex((entry) => entry.id === next.id);
  if (index < 0) {
    return [...participants, next];
  }
  return participants.map((entry, entryIndex) =>
    entryIndex === index ? next : entry,
  );
}

function participantTemplateEntries(value: unknown): ParticipantSaveParams[] {
  const record = value && typeof value === "object" ? value : {};
  const participants = Array.isArray(
    (record as { participants?: unknown }).participants,
  )
    ? (record as { participants: unknown[] }).participants
    : [];
  return participants.flatMap((entry): ParticipantSaveParams[] => {
    if (!entry || typeof entry !== "object") {
      return [];
    }
    const item = entry as Record<string, unknown>;
    const name = typeof item.name === "string" ? item.name.trim() : "";
    if (!name) {
      return [];
    }
    return [
      {
        name,
        role: typeof item.role === "string" ? item.role : undefined,
        tagline:
          typeof item.tagline === "string" ? item.tagline : undefined,
        model: typeof item.model === "string" ? item.model : undefined,
        memory: typeof item.memory === "string" ? item.memory : undefined,
      },
    ];
  });
}

export function useParticipantState({
  initialized,
  setStatus,
  archivedNoticeMs = DEFAULT_PARTICIPANT_ARCHIVED_NOTICE_MS,
}: ParticipantStateOptions): ParticipantStateController {
  const [participants, setParticipants] = useState<ParticipantProfile[]>([]);
  const [participantPanel, setParticipantPanel] = useState<
    ParticipantPanelState | undefined
  >(undefined);

  const refreshParticipants = useCallback(async (): Promise<
    ParticipantProfile[]
  > => {
    if (!initialized) {
      setParticipants([]);
      return [];
    }
    const result = await window.wuu.listParticipants();
    const nextParticipants = result.participants ?? [];
    setParticipants(nextParticipants);
    setParticipantPanel((current) => {
      if (!current?.participant?.id) {
        return current;
      }
      const fresh = nextParticipants.find(
        (participant) => participant.id === current.participant?.id,
      );
      return fresh ? { ...current, participant: fresh } : current;
    });
    return nextParticipants;
  }, [initialized]);

  async function handleParticipantDialogSave(
    params: ParticipantSaveParams,
  ): Promise<ParticipantProfile> {
    const result = await window.wuu.saveParticipant(params);
    setParticipants((current) =>
      replaceParticipantProfile(current, result.participant),
    );
    void refreshParticipants().catch(() => {
      // Best-effort roster refresh; the local upsert above is already
      // authoritative for this turn.
    });
    return result.participant;
  }

  function handleParticipantSave(params: ParticipantSaveParams): void {
    setParticipantPanel((current) =>
      current
        ? {
            ...current,
            saving: true,
            error: undefined,
          }
        : current,
    );
    void (async () => {
      try {
        const result = await window.wuu.saveParticipant(params);
        setParticipants((current) =>
          replaceParticipantProfile(current, result.participant),
        );
        setParticipantPanel({
          mode: "edit",
          participant: result.participant,
          loading: false,
        });
      } catch (error) {
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                saving: false,
                error: desktopApiErrorMessage(error, "无法保存 Agent"),
              }
            : current,
        );
      }
    })();
  }

  function handleParticipantFeedback(text: string): void {
    const participant = participantPanel?.participant;
    if (!participant) {
      return;
    }
    setParticipantPanel((current) =>
      current
        ? {
            ...current,
            feedbackSubmitting: true,
            feedbackReply: undefined,
            error: undefined,
          }
        : current,
    );
    void (async () => {
      try {
        const result = await window.wuu.sendMemoryChat({
          scope: "participant",
          participant_id: participant.id,
          message: `用户反馈：${text}`,
        });
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                feedbackSubmitting: false,
                feedbackReply: result.reply_md,
              }
            : current,
        );
      } catch (error) {
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                feedbackSubmitting: false,
                error: desktopApiErrorMessage(error, "无法写入反馈"),
              }
            : current,
        );
      }
    })();
  }

  function handleParticipantRetire(participantID: string): void {
    setParticipantPanel((current) =>
      current
        ? {
            ...current,
            retiring: true,
            error: undefined,
          }
        : current,
    );
    void (async () => {
      try {
        await window.wuu.retireParticipant(participantID);
        setParticipants((current) =>
          current.filter((entry) => entry.id !== participantID),
        );
        setParticipantPanel((current) =>
          current ? { ...current, retiring: false, archived: true } : current,
        );
        void refreshParticipants();
        window.setTimeout(() => {
          setParticipantPanel((current) =>
            current?.archived ? undefined : current,
          );
        }, archivedNoticeMs);
      } catch (error) {
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                retiring: false,
                error: desktopApiErrorMessage(error, "无法归档 Agent"),
              }
            : current,
        );
      }
    })();
  }

  function exportParticipantTemplate(): void {
    if (participants.length === 0) {
      return;
    }
    const template: ParticipantTeamTemplate = {
      version: 1,
      participants: participants.map((participant) => ({
        name: participant.name,
        role: participant.role,
        tagline: participant.tagline,
        model: participant.model,
        memory: participant.memory,
      })),
    };
    const blob = new Blob([JSON.stringify(template, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "wuu-team-template.json";
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }

  async function importParticipantTemplate(file: File): Promise<void> {
    try {
      const parsed = JSON.parse(await file.text()) as unknown;
      const entries = participantTemplateEntries(parsed);
      const existingByName = new Map(
        participants.map((participant) => [
          participant.name.trim().toLowerCase(),
          participant,
        ]),
      );
      const saved: ParticipantProfile[] = [];
      for (const entry of entries) {
        const existing = existingByName.get(entry.name.trim().toLowerCase());
        const result = await window.wuu.saveParticipant({
          ...entry,
          id: existing?.id,
        });
        saved.push(result.participant);
        existingByName.set(
          result.participant.name.trim().toLowerCase(),
          result.participant,
        );
      }
      setParticipants((current) =>
        saved.reduce(replaceParticipantProfile, current),
      );
      setStatus(`已导入 ${saved.length} 个 Agent`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "导入团队模板失败");
    }
  }

  return {
    participants,
    setParticipants,
    participantPanel,
    setParticipantPanel,
    refreshParticipants,
    handleParticipantDialogSave,
    handleParticipantSave,
    handleParticipantFeedback,
    handleParticipantRetire,
    exportParticipantTemplate,
    importParticipantTemplate,
  };
}
