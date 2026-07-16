import type { LanguagePreference } from "../shared/protocol";
import { useI18n } from "./i18n";

export function LanguagePreferenceControl(): JSX.Element {
  const { preference, setPreference, t } = useI18n();
  const options: Array<{ value: LanguagePreference; label: string }> = [
    { value: "system", label: t("common.system") },
    { value: "zh-CN", label: t("common.chinese") },
    { value: "en-US", label: t("common.english") },
  ];
  return (
    <div className="theme-segmented" role="group" aria-label={t("settings.languageGroup")}>
      {options.map((option) => (
        <button key={option.value} type="button" aria-pressed={preference === option.value}
          data-testid={`settings-language-${option.value}`} onClick={() => setPreference(option.value)}>
          {option.label}
        </button>
      ))}
    </div>
  );
}
