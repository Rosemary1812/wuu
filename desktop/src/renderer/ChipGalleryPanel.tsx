/**
 * Design-system catalog of every chip variant the renderer can produce
 * for a turn-level user-facing outcome.
 *
 * Two sections:
 *   1. Gallery: each chip rendered in isolation with its kind / tone /
 *      description. Built by calling the same `userFacingErrorForMessage`
 *      / `userFacingErrorForMissingReply` / `ContextCompactionNotice`
 *      entry points the real turn pipeline uses, so what you see here
 *      is what the user sees in the conversation.
 *   2. In-Context: three mock `Turn` records rendered through the real
 *      `TurnView` component, so the chip is shown next to a user
 *      message bubble and an assistant turn body, at the real spacing.
 *
 * Gated behind the debug-controls switch in Settings. The switch is
 * itself only visible in development builds, so this catalog never
 * reaches production. See AGENTS.md "Desktop Debug Controls".
 */
import { X } from "lucide-react";
import { type JSX } from "react";
import type { Turn } from "../shared/protocol";
import {
  userFacingErrorForMessage,
  userFacingErrorForMissingReply,
} from "./UserFacingErrors";
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { TurnView } from "./TurnView";
import { translateCurrent, useI18n } from "./i18n";

export function ChipGalleryPanel({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): JSX.Element | null {
  const { t } = useI18n();
  if (!open) {
    return null;
  }
  return (
    <div
      className="chip-gallery-backdrop"
      role="presentation"
      onClick={onClose}
    >
      <div
        className="chip-gallery-panel"
        role="dialog"
        aria-label={t("chipGallery.title")}
        aria-modal="true"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="chip-gallery-header">
          <div>
            <h2>{t("chipGallery.title")}</h2>
            <p>
              {t("chipGallery.descriptionPrefix")}{" "}
              <code>userFacingErrorForMessage</code> /
              <code>userFacingErrorForMissingReply</code> /
              <code>ContextCompactionNotice</code>{" "}{t("chipGallery.descriptionSuffix")}
            </p>
          </div>
          <button
            className="icon-button"
            type="button"
            aria-label={t("common.close")}
            onClick={onClose}
          >
            <X className="icon" />
          </button>
        </header>
        <div className="chip-gallery-body">
          <ChipGalleryGallery />
          <ChipGalleryInContext />
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Gallery section: every chip variant in isolation
// ---------------------------------------------------------------------------

type GalleryEntry = {
  /** Short label shown next to the chip. */
  label: string;
  /** `kind · tone` badge. Drives the visual contract documentation. */
  kind: string;
  /** One-line description of when this chip fires. */
  description: string;
  /** Render the actual chip using the same entry points the turn pipeline uses. */
  render: () => JSX.Element;
};

function galleryEntries(): GalleryEntry[] {
  return [
  // -----------------------------------------------------------------------
  // Soft outcomes
  // -----------------------------------------------------------------------
  {
    label: translateCurrent("chipGallery.noFinalAnswer"),
    kind: "missing_final_answer · warning",
    description: translateCurrent("chipGallery.commentaryOnly"),
    render: () => <TurnNotice display={userFacingErrorForMissingReply()} />,
  },

  // -----------------------------------------------------------------------
  // Context compaction
  // -----------------------------------------------------------------------
  {
    label: translateCurrent("chipGallery.compacting"),
    kind: "context_compacting · progress",
    description: translateCurrent("chipGallery.compactingDescription"),
    render: () => <ContextCompactionNotice status="in_progress" />,
  },
  {
    label: translateCurrent("chipGallery.compacted"),
    kind: "context_compacted · gray",
    description: translateCurrent("chipGallery.compactedDescription"),
    render: () => (
      <ContextCompactionNotice
        status="completed"
        text="✦ Compacted history: 18 → 5 messages (was ~12k tokens)"
      />
    ),
  },
  {
    label: translateCurrent("chipGallery.helpme"),
    kind: "context_helpme · gray",
    description: translateCurrent("chipGallery.helpmeDescription"),
    render: () => (
      <ContextCompactionNotice
        status="completed"
        reason="helpme"
        text="✦ HelpMe recovered and compacted history: 42 → 2 messages (was ~90k tokens)"
      />
    ),
  },
  {
    label: translateCurrent("chipGallery.compactionFailed"),
    kind: "context_compaction_failed · gray",
    description: translateCurrent("chipGallery.compactionFailedDescription"),
    render: () => (
      <ContextCompactionNotice
        status="completed"
        text="Context compaction failed; continuing without compacting history."
      />
    ),
  },

  // -----------------------------------------------------------------------
  // Provider errors (error tone)
  // -----------------------------------------------------------------------
  {
    label: "context_length_exceeded",
    kind: "provider · error",
    description: translateCurrent("chipGallery.contextExceeded"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage(
          "context_length_exceeded: Your input exceeds the context window",
          "turn",
        )}
      />
    ),
  },
  {
    label: "stream_closed_before_response.completed",
    kind: "provider · error",
    description: translateCurrent("chipGallery.streamClosed"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage(
          "stream request failed: websocket stream closed before response.completed",
          "turn",
        )}
      />
    ),
  },
  {
    label: "connection reset",
    kind: "network · error",
    description: translateCurrent("chipGallery.connectionReset"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("connection reset by peer", "turn")}
      />
    ),
  },
  {
    label: "timeout",
    kind: "network · error",
    description: translateCurrent("chipGallery.timeout"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("deadline exceeded", "turn")}
      />
    ),
  },

  // -----------------------------------------------------------------------
  // Auth (auth tone)
  // -----------------------------------------------------------------------
  {
    label: "401 unauthorized",
    kind: "auth · auth",
    description: translateCurrent("chipGallery.invalidCredentials"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("401 unauthorized", "turn")}
      />
    ),
  },
  {
    label: "403 forbidden",
    kind: "auth · auth",
    description: translateCurrent("chipGallery.insufficientPermissions"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("403 forbidden", "turn")}
      />
    ),
  },

  // -----------------------------------------------------------------------
  // Tool / Internal (error tone)
  // -----------------------------------------------------------------------
  {
    label: "command failed",
    kind: "local · error",
    description: translateCurrent("chipGallery.commandFailed"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage(
          "command failed: exit status 1: cat: /nonexistent: No such file or directory",
          "turn",
        )}
      />
    ),
  },
  {
    label: "panic: nil pointer",
    kind: "internal · error",
    description: translateCurrent("chipGallery.internalError"),
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("panic: nil pointer", "turn")}
      />
    ),
  },
  ];
}

function ChipGalleryGallery(): JSX.Element {
  const { t } = useI18n();
  const entries = galleryEntries();
  return (
    <section className="chip-gallery-section">
      <header>
        <h3>{t("chipGallery.galleryTitle")}</h3>
        <p>
          {t("chipGallery.galleryDescriptionPrefix")} <code>kind · tone</code>{" "}
          {t("chipGallery.galleryDescriptionSuffix")}
        </p>
      </header>
      <ul className="chip-gallery-entries">
        {entries.map((entry, index) => (
          <li
            key={`${entry.kind}-${index}`}
            className="chip-gallery-entry"
            data-kind={entry.kind}
          >
            <div className="chip-gallery-entry-chip">{entry.render()}</div>
            <div className="chip-gallery-entry-meta">
              <strong className="chip-gallery-entry-label">{entry.label}</strong>
              <code className="chip-gallery-entry-kind">{entry.kind}</code>
              <span className="chip-gallery-entry-description">
                {entry.description}
              </span>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

// ---------------------------------------------------------------------------
// In-Context section: chips as they appear in the conversation stream
// ---------------------------------------------------------------------------

type ContextTurn = {
  /** Section heading shown above the mock turn. */
  heading: string;
  /** The mock turn rendered through the real `TurnView` component. */
  turn: Turn;
};

/**
 * Three representative scenarios. Each one constructs a `Turn` with the
 * minimum items needed to surface the target chip via the real
 * `turnEventForTurn` / `turnEventForItem` pipeline.
 */
function contextTurns(): ContextTurn[] {
  return [
  {
    heading: translateCurrent("chipGallery.commentaryOnly"),
    turn: {
      id: "demo-missing-reply",
      items: [
        userMessage(translateCurrent("chipGallery.mock.summarize")),
        commentary(translateCurrent("chipGallery.mock.review")),
        commentary(translateCurrent("chipGallery.mock.keyPoints")),
        commentary(translateCurrent("chipGallery.mock.thirdFile")),
      ],
      items_view: "full",
      status: "completed",
    },
  },
  {
    heading: translateCurrent("chipGallery.contextTurn"),
    turn: {
      id: "demo-compact",
      items: [
        userMessage(translateCurrent("chipGallery.mock.continueWork")),
        {
          id: "compact-1",
          type: "context_compaction",
          status: "completed",
          text: "✦ Compacted history: 18 → 5 messages (was ~12k tokens)",
        },
        finalAnswer(translateCurrent("chipGallery.mock.continueAnswer")),
      ],
      items_view: "full",
      status: "completed",
    },
  },
  {
    heading: translateCurrent("chipGallery.providerError"),
    turn: {
      id: "demo-failed",
      items: [userMessage(translateCurrent("chipGallery.mock.analyzeLargeFile"))],
      items_view: "full",
      status: "failed",
      error: {
        message: "context_length_exceeded: Your input exceeds the context window",
        code: "context_length_exceeded",
        category: "provider",
      },
    },
  },
  ];
}

function userMessage(text: string): Turn["items"][number] {
  return {
    id: `u-${text.slice(0, 8)}`,
    type: "user_message",
    role: "user",
    status: "completed",
    text,
  };
}

function commentary(text: string): Turn["items"][number] {
  return {
    id: `c-${text.slice(0, 8)}`,
    type: "agent_message",
    role: "assistant",
    phase: "commentary",
    status: "completed",
    text,
  };
}

function finalAnswer(text: string): Turn["items"][number] {
  return {
    id: `f-${text.slice(0, 8)}`,
    type: "agent_message",
    role: "assistant",
    phase: "final_answer",
    status: "completed",
    text,
  };
}

function ChipGalleryInContext(): JSX.Element {
  const { t } = useI18n();
  const turns = contextTurns();
  return (
    <section className="chip-gallery-section">
      <header>
        <h3>{t("chipGallery.inContextTitle")}</h3>
        <p>
          {t("chipGallery.inContextDescriptionPrefix")} <code>TurnView</code>{" "}
          {t("chipGallery.inContextDescriptionSuffix")}
        </p>
      </header>
      <ul className="chip-gallery-context">
        {turns.map((entry) => (
          <li
            key={entry.turn.id}
            className="chip-gallery-context-item"
            data-heading={entry.heading}
          >
            <h4>{entry.heading}</h4>
            <div className="chip-gallery-context-frame">
              <TurnView
                turn={entry.turn}
                onStreamFrame={() => {}}
              />
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
