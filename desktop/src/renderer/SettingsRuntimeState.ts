import { useCallback, useEffect, useState } from "react";
import type {
  CodexPetSettingsUpdate,
  CodexPetsSnapshot,
  SettingsUsageResponse,
} from "../shared/protocol";
import { translateCurrent } from "./i18n";

export type SettingsRuntimeState = {
  settingsUsage: SettingsUsageResponse | undefined;
  settingsUsageLoading: boolean;
  settingsUsageError: string;
  codexPets: CodexPetsSnapshot | undefined;
  codexPetsLoading: boolean;
  codexPetsError: string;
  refreshCodexPets: () => Promise<CodexPetsSnapshot>;
  updateCodexPets: (
    settings: CodexPetSettingsUpdate,
  ) => Promise<CodexPetsSnapshot>;
};

function codexPetsUnsupportedMessage(): string {
  return translateCurrent("settings.pets.unsupported");
}

export function useSettingsRuntimeState({
  settingsOpen,
}: {
  settingsOpen: boolean;
}): SettingsRuntimeState {
  const [settingsUsage, setSettingsUsage] = useState<
    SettingsUsageResponse | undefined
  >(undefined);
  const [settingsUsageLoading, setSettingsUsageLoading] = useState(false);
  const [settingsUsageError, setSettingsUsageError] = useState("");
  const [codexPets, setCodexPets] = useState<CodexPetsSnapshot | undefined>();
  const [codexPetsLoading, setCodexPetsLoading] = useState(true);
  const [codexPetsError, setCodexPetsError] = useState("");

  const refreshCodexPets =
    useCallback(async (): Promise<CodexPetsSnapshot> => {
      const api = window.wuu as Partial<typeof window.wuu>;
      if (typeof api.listCodexPets !== "function") {
        setCodexPetsError(codexPetsUnsupportedMessage());
        setCodexPetsLoading(false);
        throw new Error(codexPetsUnsupportedMessage());
      }
      setCodexPetsLoading(true);
      setCodexPetsError("");
      try {
        const snapshot = await api.listCodexPets();
        setCodexPets(snapshot);
        return snapshot;
      } catch (error) {
        const message =
          error instanceof Error ? error.message : translateCurrent("settings.pets.readFailed");
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
        setCodexPetsError(codexPetsUnsupportedMessage());
        setCodexPetsLoading(false);
        throw new Error(codexPetsUnsupportedMessage());
      }
      setCodexPetsLoading(true);
      setCodexPetsError("");
      try {
        const snapshot = await api.updateCodexPetSettings(settings);
        setCodexPets(snapshot);
        return snapshot;
      } catch (error) {
        const message =
          error instanceof Error ? error.message : translateCurrent("settings.pets.saveFailed");
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
      setSettingsUsageLoading(false);
      setSettingsUsageError("");
      return;
    }
    const api = window.wuu as Partial<typeof window.wuu>;
    if (typeof api.getSettingsUsage !== "function") {
      setSettingsUsage(undefined);
      setSettingsUsageLoading(false);
      setSettingsUsageError(translateCurrent("settings.usageUnsupported"));
      return;
    }
    let cancelled = false;
    setSettingsUsageLoading(true);
    setSettingsUsageError("");
    void api
      .getSettingsUsage()
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
        setSettingsUsageError(translateCurrent("settings.usageLoadFailed"));
      })
      .finally(() => {
        if (!cancelled) {
          setSettingsUsageLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [settingsOpen]);

  useEffect(() => {
    let cancelled = false;
    const api = window.wuu as Partial<typeof window.wuu>;
    if (typeof api.listCodexPets !== "function") {
      setCodexPetsLoading(false);
      setCodexPetsError(codexPetsUnsupportedMessage());
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
            error instanceof Error ? error.message : translateCurrent("settings.pets.readFailed"),
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
    settingsUsage,
    settingsUsageLoading,
    settingsUsageError,
    codexPets,
    codexPetsLoading,
    codexPetsError,
    refreshCodexPets,
    updateCodexPets,
  };
}
