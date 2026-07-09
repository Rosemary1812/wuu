import {
  type Dispatch,
  type SetStateAction,
  useCallback,
  useEffect,
  useState,
} from "react";
import type {
  CodexPetSettingsUpdate,
  CodexPetsSnapshot,
  SettingsUsageRange,
  SettingsUsageResponse,
} from "../shared/protocol";

export type SettingsRuntimeState = {
  usageRange: SettingsUsageRange;
  setUsageRange: Dispatch<SetStateAction<SettingsUsageRange>>;
  settingsUsage: SettingsUsageResponse | undefined;
  codexPets: CodexPetsSnapshot | undefined;
  codexPetsLoading: boolean;
  codexPetsError: string;
  refreshCodexPets: () => Promise<CodexPetsSnapshot>;
  updateCodexPets: (
    settings: CodexPetSettingsUpdate,
  ) => Promise<CodexPetsSnapshot>;
};

const CODEX_PETS_UNSUPPORTED_MESSAGE =
  "当前桌面进程不支持 Codex Pets，请重启应用";

export function useSettingsRuntimeState({
  settingsOpen,
}: {
  settingsOpen: boolean;
}): SettingsRuntimeState {
  const [usageRange, setUsageRange] = useState<SettingsUsageRange>("all");
  const [settingsUsage, setSettingsUsage] = useState<
    SettingsUsageResponse | undefined
  >(undefined);
  const [codexPets, setCodexPets] = useState<CodexPetsSnapshot | undefined>();
  const [codexPetsLoading, setCodexPetsLoading] = useState(true);
  const [codexPetsError, setCodexPetsError] = useState("");

  const refreshCodexPets =
    useCallback(async (): Promise<CodexPetsSnapshot> => {
      const api = window.wuu as Partial<typeof window.wuu>;
      if (typeof api.listCodexPets !== "function") {
        setCodexPetsError(CODEX_PETS_UNSUPPORTED_MESSAGE);
        setCodexPetsLoading(false);
        throw new Error(CODEX_PETS_UNSUPPORTED_MESSAGE);
      }
      setCodexPetsLoading(true);
      setCodexPetsError("");
      try {
        const snapshot = await api.listCodexPets();
        setCodexPets(snapshot);
        return snapshot;
      } catch (error) {
        const message =
          error instanceof Error ? error.message : "无法读取 Codex Pets";
        setCodexPetsError(message);
        throw error;
      } finally {
        setCodexPetsLoading(false);
      }
    }, []);

  const updateCodexPets = useCallback(
    async (
      settings: CodexPetSettingsUpdate,
    ): Promise<CodexPetsSnapshot> => {
      const api = window.wuu as Partial<typeof window.wuu>;
      if (typeof api.updateCodexPetSettings !== "function") {
        setCodexPetsError(CODEX_PETS_UNSUPPORTED_MESSAGE);
        setCodexPetsLoading(false);
        throw new Error(CODEX_PETS_UNSUPPORTED_MESSAGE);
      }
      setCodexPetsLoading(true);
      setCodexPetsError("");
      try {
        const snapshot = await api.updateCodexPetSettings(settings);
        setCodexPets(snapshot);
        return snapshot;
      } catch (error) {
        const message =
          error instanceof Error ? error.message : "无法保存 Codex Pet";
        setCodexPetsError(message);
        throw error;
      } finally {
        setCodexPetsLoading(false);
      }
    },
    [],
  );

  useEffect(() => {
    if (!settingsOpen) {
      setSettingsUsage(undefined);
      return;
    }
    let cancelled = false;
    void window.wuu
      .getSettingsUsage(usageRange)
      .then((response) => {
        if (cancelled) {
          return;
        }
        setSettingsUsage(response);
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        setSettingsUsage(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [settingsOpen, usageRange]);

  useEffect(() => {
    let cancelled = false;
    const api = window.wuu as Partial<typeof window.wuu>;
    if (typeof api.listCodexPets !== "function") {
      setCodexPetsLoading(false);
      setCodexPetsError(CODEX_PETS_UNSUPPORTED_MESSAGE);
      return () => {
        cancelled = true;
      };
    }
    setCodexPetsLoading(true);
    setCodexPetsError("");
    void api
      .listCodexPets()
      .then((snapshot) => {
        if (!cancelled) {
          setCodexPets(snapshot);
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setCodexPetsError(
            error instanceof Error ? error.message : "无法读取 Codex Pets",
          );
        }
      })
      .finally(() => {
        if (!cancelled) {
          setCodexPetsLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return {
    usageRange,
    setUsageRange,
    settingsUsage,
    codexPets,
    codexPetsLoading,
    codexPetsError,
    refreshCodexPets,
    updateCodexPets,
  };
}
