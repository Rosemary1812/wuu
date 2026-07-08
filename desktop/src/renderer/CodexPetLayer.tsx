import type {
  CodexPet,
  CodexPetsSnapshot,
  CodexPetState,
  CodexPetStateID,
} from "../shared/protocol";
import type { CSSProperties } from "react";

const CODEX_PET_CELL_WIDTH = 192;
const CODEX_PET_CELL_HEIGHT = 208;

const CODEX_PET_STATES: CodexPetState[] = [
  { id: "idle", label: "Idle", row: 0, frames: 6 },
  { id: "running-right", label: "Run right", row: 1, frames: 8 },
  { id: "running-left", label: "Run left", row: 2, frames: 8 },
  { id: "waving", label: "Waving", row: 3, frames: 4 },
  { id: "jumping", label: "Jumping", row: 4, frames: 5 },
  { id: "failed", label: "Failed", row: 5, frames: 8 },
  { id: "waiting", label: "Waiting", row: 6, frames: 6 },
  { id: "running", label: "Running", row: 7, frames: 6 },
  { id: "review", label: "Review", row: 8, frames: 6 },
];

const STATE_BY_ID = new Map<CodexPetStateID, CodexPetState>(
  CODEX_PET_STATES.map((state) => [state.id, state]),
);

export function codexPetStateForRuntime({
  running,
  status,
}: {
  running: boolean;
  status: string;
}): CodexPetState {
  const normalizedStatus = status.trim().toLowerCase();
  if (/\b(failed|error)\b|失败|错误/.test(normalizedStatus)) {
    return stateByID("failed");
  }
  if (/权限|批准|确认|permission|approve|review/.test(normalizedStatus)) {
    return stateByID("review");
  }
  if (running) {
    return stateByID("running");
  }
  if (/等待|排队|queued|waiting/.test(normalizedStatus)) {
    return stateByID("waiting");
  }
  return stateByID("idle");
}

export function CodexPetLayer({
  snapshot,
  running,
  status,
}: {
  snapshot: CodexPetsSnapshot | undefined;
  running: boolean;
  status: string;
}): JSX.Element | null {
  const pet = selectedPet(snapshot);
  if (!pet || !snapshot?.enabled) {
    return null;
  }
  const state = codexPetStateForRuntime({ running, status });
  const style = {
    backgroundImage: `url(${pet.spritesheet_url})`,
    "--codex-pet-y": `${state.row * -CODEX_PET_CELL_HEIGHT}px`,
    "--codex-pet-end-x": `${state.frames * -CODEX_PET_CELL_WIDTH}px`,
    "--codex-pet-frames": state.frames,
    "--codex-pet-duration": `${Math.max(state.frames * 260, 1400)}ms`,
  } as CSSProperties;

  return (
    <aside className="codex-pet-layer" aria-live="polite">
      <div
        className="codex-pet-sprite"
        aria-label={`${pet.display_name} ${state.id}`}
        style={style}
      />
    </aside>
  );
}

function selectedPet(snapshot: CodexPetsSnapshot | undefined): CodexPet | undefined {
  if (!snapshot) {
    return undefined;
  }
  return snapshot.pets.find((pet) => pet.id === snapshot.selected_id) ?? snapshot.pets[0];
}

function stateByID(id: CodexPetStateID): CodexPetState {
  return STATE_BY_ID.get(id) ?? CODEX_PET_STATES[0];
}
