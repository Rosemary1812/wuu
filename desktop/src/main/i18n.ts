import type { AppLocale, LanguagePreference } from "../shared/protocol";

const resources = {
  "zh-CN": {
    conversation: "对话",
    resize: "调整大小",
    pet: "桌宠",
    openConversation: "打开会话 · {title}",
    closePet: "关闭桌宠",
    chooseExistingFolder: "使用现有文件夹",
    useFolder: "使用文件夹",
    createBlankProject: "新建空白项目",
    createProject: "创建项目",
    relocateWorkspace: "重新定位工作区",
    relocateHere: "定位到此文件夹",
    open: "打开",
    openInApplication: "在 {application} 中打开",
    openWith: "打开方式",
    copyPath: "复制路径",
    projectUnavailable:
      "工作区目录当前不可用：{path}。请恢复该目录，或从工作区菜单选择“重新定位…”。",
  },
  "en-US": {
    conversation: "Conversation",
    resize: "Resize",
    pet: "Desktop pet",
    openConversation: "Open conversation · {title}",
    closePet: "Close desktop pet",
    chooseExistingFolder: "Use an existing folder",
    useFolder: "Use folder",
    createBlankProject: "Create a blank project",
    createProject: "Create project",
    relocateWorkspace: "Relocate workspace",
    relocateHere: "Use this folder",
    open: "Open",
    openInApplication: "Open in {application}",
    openWith: "Open With",
    copyPath: "Copy Path",
    projectUnavailable:
      "The workspace folder is currently unavailable: {path}. Restore the folder or choose Relocate from the workspace menu.",
  },
} as const;

export type MainTranslationKey = keyof (typeof resources)["zh-CN"];

let activeLocale: AppLocale = "zh-CN";

export function resolveMainLocale(
  preference: LanguagePreference,
  systemLocale: string,
): AppLocale {
  if (preference !== "system") return preference;
  return systemLocale.toLowerCase().startsWith("zh") ? "zh-CN" : "en-US";
}

export function setMainLocale(locale: AppLocale): void {
  activeLocale = locale;
}

export function getMainLocale(): AppLocale {
  return activeLocale;
}

export function mainTranslate(
  key: MainTranslationKey,
  values: Record<string, string | number> = {},
  locale: AppLocale = activeLocale,
): string {
  return resources[locale][key].replace(
    /\{(\w+)\}/g,
    (_, name: string) => String(values[name] ?? `{${name}}`),
  );
}
