import { useEffect, useMemo, useRef, useState } from "react";
import { X } from "lucide-react";
import { SelectMenu } from "./SelectMenu";
import { useI18n } from "./i18n";
import { providerModelVariantOptions, variantLabel } from "./RuntimeHelpers";
import type {
  ModelAliasSummary,
  ProviderSummary,
  RuntimeAdvancedSettingsUpdate,
} from "../shared/protocol";

export type SubagentModelAliasesProps = {
  aliases: Record<string, ModelAliasSummary> | undefined;
  providers: ProviderSummary[];
  disabled?: boolean;
  onSave: (update: RuntimeAdvancedSettingsUpdate) => Promise<void>;
};

type AliasDraft = {
  key: string;
  alias: string;
  provider: string;
  model: string;
  variant: string;
};

type AliasValidationError = {
  alias?: string;
  provider?: string;
  model?: string;
  variant?: string;
};

const ALIAS_RE = /^[a-z][a-z0-9_-]*$/;

function generateKey(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function emptyDraft(): AliasDraft {
  return {
    key: generateKey(),
    alias: "",
    provider: "",
    model: "",
    variant: "",
  };
}

function stableStringify(value: unknown): string {
  return JSON.stringify(value, (_key, val) => {
    if (val && typeof val === "object" && !Array.isArray(val)) {
      return Object.fromEntries(
        Object.entries(val as Record<string, unknown>).sort(([a], [b]) =>
          a.localeCompare(b),
        ),
      );
    }
    return val;
  });
}

function aliasVariantOrEffort(
  provider: ProviderSummary | undefined,
  modelID: string,
  value: string,
): Partial<ModelAliasSummary> {
  if (!value) return {};
  const model = provider?.models?.find((item) => item.id === modelID);
  if (model?.variants?.some((item) => item.id === value)) {
    return { variant: value };
  }
  if (model?.supported_efforts?.includes(value)) {
    return { effort: value };
  }
  // Unknown catalog or reasoning toggle; default to variant so a non-empty
  // value is preserved and the backend can validate.
  return { variant: value };
}

function isValidVariant(
  provider: ProviderSummary | undefined,
  modelID: string,
  variant: string,
): boolean {
  if (!variant) return true;
  const model = provider?.models?.find((item) => item.id === modelID);
  if (!model) return true; // Unknown model: defer to backend validation.
  const variantIDs = (model.variants ?? []).map((item) => item.id);
  const efforts = model.supported_efforts ?? [];
  if (variantIDs.length > 0 || efforts.length > 0) {
    return variantIDs.includes(variant) || efforts.includes(variant);
  }
  if (model.capabilities?.reasoning === true) return variant === "none";
  return true;
}

export function SubagentModelAliases({
  aliases,
  providers,
  disabled,
  onSave,
}: SubagentModelAliasesProps): JSX.Element {
  const { t } = useI18n();
  const [rows, setRows] = useState<AliasDraft[]>([]);
  const [errors, setErrors] = useState<Record<string, AliasValidationError>>({});
  const [saveError, setSaveError] = useState("");
  const [saving, setSaving] = useState(false);
  const lastAliasesJsonRef = useRef("");

  const providerOptions = useMemo(
    () => providers.map((provider) => ({ value: provider.name, label: provider.name })),
    [providers],
  );

  useEffect(() => {
    const json = stableStringify(aliases);
    if (json === lastAliasesJsonRef.current) return;
    lastAliasesJsonRef.current = json;

    const nextRows: AliasDraft[] = [];
    if (aliases) {
      for (const [alias, config] of Object.entries(aliases)) {
        nextRows.push({
          key: generateKey(),
          alias,
          provider: config.provider,
          model: config.model,
          variant: config.variant ?? config.effort ?? "",
        });
      }
    }
    setRows(nextRows);
    setErrors({});
    setSaveError("");
  }, [aliases]);

  function updateRow(key: string, patch: Partial<AliasDraft>): void {
    setRows((prev) =>
      prev.map((row) => (row.key === key ? { ...row, ...patch } : row)),
    );
    setErrors((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  }

  function addRow(): void {
    setRows((prev) => [...prev, emptyDraft()]);
  }

  function removeRow(key: string): void {
    setRows((prev) => prev.filter((row) => row.key !== key));
    setErrors((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  }

  function validateRows(): {
    valid: boolean;
    errors: Record<string, AliasValidationError>;
    normalized: Record<string, ModelAliasSummary>;
  } {
    const nextErrors: Record<string, AliasValidationError> = {};
    const normalized: Record<string, ModelAliasSummary> = {};
    const seen = new Set<string>();
    let valid = true;

    for (const row of rows) {
      const err: AliasValidationError = {};
      const alias = row.alias.trim();

      if (!alias) {
        err.alias = t("subagentAlias.invalidAlias");
        valid = false;
      } else if (!ALIAS_RE.test(alias)) {
        err.alias = t("subagentAlias.invalidAlias");
        valid = false;
      } else if (seen.has(alias)) {
        err.alias = t("subagentAlias.duplicateAlias");
        valid = false;
      } else {
        seen.add(alias);
      }

      const providerName = row.provider.trim();
      if (!providerName) {
        err.provider = t("subagentAlias.emptyProvider");
        valid = false;
      } else if (!providers.some((p) => p.name === providerName)) {
        err.provider = t("subagentAlias.emptyProvider");
        valid = false;
      }

      const model = row.model.trim();
      if (!model) {
        err.model = t("subagentAlias.emptyModel");
        valid = false;
      }

      const provider = providers.find((p) => p.name === providerName);
      const variant = row.variant.trim();
      if (variant && !isValidVariant(provider, model, variant)) {
        err.variant = t("subagentAlias.invalidVariant");
        valid = false;
      }

      nextErrors[row.key] = err;

      if (alias && !err.alias && !err.provider && !err.model && !err.variant) {
        normalized[alias] = {
          provider: providerName,
          model,
          ...aliasVariantOrEffort(provider, model, variant),
        };
      }
    }

    return { valid, errors: nextErrors, normalized };
  }

  function hasChanges(normalized: Record<string, ModelAliasSummary>): boolean {
    if (!aliases) return Object.keys(normalized).length > 0;
    return stableStringify(normalized) !== stableStringify(aliases);
  }

  async function handleSave(): Promise<void> {
    if (disabled || saving) return;
    setSaveError("");
    const { valid, errors: nextErrors, normalized } = validateRows();
    if (!valid) {
      setErrors(nextErrors);
      return;
    }
    if (!hasChanges(normalized)) return;

    setSaving(true);
    try {
      await onSave({ model_aliases: normalized });
      lastAliasesJsonRef.current = stableStringify(normalized);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("subagentAlias.saveFailed"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="settings-section" data-testid="settings-subagent-aliases">
      <header className="settings-alias-toolbar">
        <div className="settings-alias-heading">
          <h2 className="settings-section-title">{t("settings.subagentModelAliases")}</h2>
          <p className="settings-section-description">{t("settings.subagentModelAliasesHint")}</p>
        </div>
        <div className="settings-alias-actions">
          <button
            className="settings-button"
            type="button"
            disabled={disabled || saving}
            onClick={addRow}
          >
            {t("subagentAlias.add")}
          </button>
          <button
            className="settings-button settings-button-primary"
            type="button"
            disabled={disabled || saving}
            onClick={() => void handleSave()}
          >
            {t("subagentAlias.save")}
          </button>
          {saveError ? <span className="settings-error">{saveError}</span> : null}
        </div>
      </header>
      <div className="settings-card">
        <div className="settings-alias-list">
          {rows.map((row) => {
            const provider = providers.find((p) => p.name === row.provider.trim());
            const variantOptions = provider
              ? providerModelVariantOptions(provider, row.model.trim(), row.variant)
              : [];
            const rowErrors = errors[row.key] ?? {};
            return (
              <div className="settings-alias-row" key={row.key}>
                <div className="settings-alias-field">
                  <input
                    className="settings-input"
                    value={row.alias}
                    placeholder={t("subagentAlias.alias")}
                    disabled={disabled || saving}
                    onChange={(event) => updateRow(row.key, { alias: event.target.value })}
                    aria-invalid={Boolean(rowErrors.alias)}
                    aria-label={t("subagentAlias.alias")}
                  />
                  {rowErrors.alias ? (
                    <span className="settings-alias-field-error">{rowErrors.alias}</span>
                  ) : null}
                </div>
                <div className="settings-alias-field">
                  <SelectMenu
                    triggerClassName="settings-select-trigger"
                    ariaLabel={t("subagentAlias.provider")}
                    dataTestid={`subagent-alias-provider-select`}
                    value={row.provider}
                    placeholder={t("subagentAlias.provider")}
                    disabled={disabled || saving}
                    options={providerOptions}
                    onChange={(value) =>
                      updateRow(row.key, { provider: value })
                    }
                  />
                  {rowErrors.provider ? (
                    <span className="settings-alias-field-error">{rowErrors.provider}</span>
                  ) : null}
                </div>
                <div className="settings-alias-field">
                  <input
                    className="settings-input"
                    value={row.model}
                    placeholder={t("subagentAlias.model")}
                    disabled={disabled || saving}
                    onChange={(event) => updateRow(row.key, { model: event.target.value })}
                    aria-invalid={Boolean(rowErrors.model)}
                    aria-label={t("subagentAlias.model")}
                  />
                  {rowErrors.model ? (
                    <span className="settings-alias-field-error">{rowErrors.model}</span>
                  ) : null}
                </div>
                <div className="settings-alias-field">
                  <SelectMenu
                    triggerClassName="settings-select-trigger"
                    ariaLabel={t("subagentAlias.reasoning")}
                    dataTestid={`subagent-alias-reasoning-select`}
                    value={row.variant}
                    placeholder={t("subagentAlias.reasoning")}
                    disabled={disabled || saving || variantOptions.length === 0}
                    options={variantOptions.map((variant) => ({
                      value: variant,
                      label: variantLabel(variant),
                    }))}
                    onChange={(value) => updateRow(row.key, { variant: value })}
                  />
                  {rowErrors.variant ? (
                    <span className="settings-alias-field-error">{rowErrors.variant}</span>
                  ) : null}
                </div>
                <button
                  className="settings-alias-remove"
                  type="button"
                  title={t("subagentAlias.remove")}
                  aria-label={t("subagentAlias.remove")}
                  disabled={disabled || saving}
                  onClick={() => removeRow(row.key)}
                >
                  <X className="icon" aria-hidden="true" />
                </button>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
