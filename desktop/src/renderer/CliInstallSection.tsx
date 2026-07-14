import { useEffect, useState } from "react";
import type { CliInstallStatus } from "../shared/protocol";

// CliInstallSection renders the settings block for the wuu CLI symlink
// (~/.local/bin/wuu, like VS Code's "install 'code' command"). Install now
// runs automatically at app startup (main process, default on): idempotent,
// self-repairing after an app move/update, but never overwriting a foreign
// wuu install. This section surfaces that state, hosts the auto-install
// toggle, and keeps the manual install / overwrite flow for the cases the
// automatic pass deliberately leaves alone.

type LoadState = {
  loading: boolean;
  status?: CliInstallStatus;
  error: string;
};

export function CliInstallSection(): JSX.Element {
  const [state, setState] = useState<LoadState>({ loading: true, error: "" });
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState("");
  const [actionError, setActionError] = useState("");
  const [needsOverwrite, setNeedsOverwrite] = useState(false);
  const [autoEnabled, setAutoEnabled] = useState(true);
  const [autoToggleBusy, setAutoToggleBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void loadStatus(() => cancelled);
    return () => {
      cancelled = true;
    };

    async function loadStatus(isCancelled: () => boolean): Promise<void> {
      if (typeof window.wuu?.getCliInstallStatus !== "function") {
        if (!isCancelled()) {
          setState({ loading: false, error: "" });
        }
        return;
      }
      if (!isCancelled()) {
        setState((current) => ({ ...current, loading: true, error: "" }));
      }
      try {
        const status = await window.wuu.getCliInstallStatus();
        if (!isCancelled()) {
          setState({ loading: false, error: "", status });
          setAutoEnabled(status.auto_install_enabled);
        }
      } catch (error) {
        if (!isCancelled()) {
          setState({
            loading: false,
            error: error instanceof Error ? error.message : "无法读取命令行安装状态",
          });
        }
      }
    }
  }, []);

  async function refreshStatus(): Promise<void> {
    if (typeof window.wuu?.getCliInstallStatus !== "function") {
      return;
    }
    try {
      const status = await window.wuu.getCliInstallStatus();
      setState({ loading: false, error: "", status });
      setAutoEnabled(status.auto_install_enabled);
    } catch {
      // Keep the previous status; the action's own error message covers this.
    }
  }

  async function runInstall(overwrite: boolean): Promise<void> {
    if (typeof window.wuu?.installCli !== "function") {
      return;
    }
    setBusy(true);
    setActionError("");
    setFeedback("");
    try {
      const result = await window.wuu.installCli(overwrite);
      if (result.needs_overwrite) {
        setNeedsOverwrite(true);
        return;
      }
      setNeedsOverwrite(false);
      if (!result.ok) {
        setActionError(result.message ?? "安装失败");
        return;
      }
      setFeedback(
        result.on_path
          ? "已安装到 ~/.local/bin/wuu，现在可以在终端直接运行 wuu。"
          : "已安装到 ~/.local/bin/wuu。",
      );
      await refreshStatus();
    } catch (error) {
      setActionError(error instanceof Error ? error.message : "安装失败");
    } finally {
      setBusy(false);
    }
  }

  async function toggleAutoInstall(): Promise<void> {
    if (typeof window.wuu?.setCliAutoInstallEnabled !== "function") {
      return;
    }
    const next = !autoEnabled;
    setAutoEnabled(next);
    setAutoToggleBusy(true);
    setActionError("");
    try {
      await window.wuu.setCliAutoInstallEnabled(next);
    } catch (error) {
      setAutoEnabled(!next);
      setActionError(error instanceof Error ? error.message : "保存设置失败");
    } finally {
      setAutoToggleBusy(false);
    }
  }

  const status = state.status;
  const supported = status?.platform_supported ?? true;
  const sourceMissing = Boolean(status && status.source_path === null);
  const installPath = status?.install_path ?? "~/.local/bin/wuu";
  const alreadyLinked = Boolean(status?.linked_to_source);
  const foreignInstall = Boolean(status?.foreign_install);
  const showPathHint = Boolean(status && !status.on_path && supported);
  const autoOutcome = status?.last_auto_install?.outcome;
  const autoJustInstalled = autoOutcome === "installed" || autoOutcome === "repaired";

  return (
    <section className="settings-section" data-testid="settings-cli-install">
      <header className="settings-section-header">
        <h2 className="settings-section-title">命令行工具</h2>
      </header>
      <div className="settings-card">
        <div className="settings-row">
          <div className="settings-row-label">
            <span className="settings-row-label-title">安装位置</span>
          </div>
          <span className="settings-row-control-value">{installPath}</span>
        </div>

        {state.loading ? (
          <div className="settings-row">
            <div className="settings-row-label">
              <span className="settings-row-label-description">正在读取安装状态…</span>
            </div>
          </div>
        ) : null}

        {state.error ? <div className="settings-error">{state.error}</div> : null}

        {!state.loading && !supported ? (
          <div className="settings-row">
            <div className="settings-row-label">
              <span className="settings-row-label-description">
                暂不支持在当前系统安装命令行工具（仅支持 macOS / Linux）。
              </span>
            </div>
          </div>
        ) : null}

        {!state.loading && supported ? (
          <div className="settings-row">
            <div className="settings-row-label">
              <span className="settings-row-label-title">启动时自动安装</span>
              <span className="settings-row-label-description">
                每次启动检查并自动创建 / 修复 wuu 命令链接；不会覆盖非桌面版安装的 wuu。
              </span>
            </div>
            <button
              className="settings-switch"
              type="button"
              role="switch"
              aria-checked={autoEnabled}
              disabled={autoToggleBusy || !status}
              data-testid="cli-auto-install-switch"
              onClick={() => void toggleAutoInstall()}
            >
              <span className="settings-switch-thumb" aria-hidden="true" />
              <span className="sr-only">
                {autoEnabled ? "关闭启动时自动安装" : "开启启动时自动安装"}
              </span>
            </button>
          </div>
        ) : null}

        {!state.loading && supported && autoJustInstalled ? (
          <div className="settings-saved" data-testid="cli-install-auto-note">
            {autoOutcome === "repaired"
              ? "启动时已自动修复 wuu 命令链接。"
              : "启动时已自动安装 wuu 命令到 ~/.local/bin/wuu。"}
          </div>
        ) : null}

        {!state.loading && supported && foreignInstall ? (
          <div className="settings-row" data-testid="cli-install-foreign">
            <div className="settings-row-label">
              <span className="settings-row-label-title">检测到已有 wuu 命令</span>
              <span className="settings-row-label-description">
                {installPath} 已存在且不是桌面版创建的链接（可能来自 go install 或
                install.sh），桌面版未接管。如需改为使用桌面版自带的 wuu，请手动安装并确认覆盖。
              </span>
            </div>
          </div>
        ) : null}

        {!state.loading && supported ? (
          <div className="settings-row">
            <div className="settings-row-label">
              <span className="settings-row-label-title">
                {alreadyLinked ? "已安装" : "手动安装 wuu 命令"}
              </span>
              <span className="settings-row-label-description">
                {sourceMissing
                  ? "找不到 wuu 可执行文件，无法创建链接。"
                  : alreadyLinked
                    ? "已链接到当前 wuu 可执行文件。可重新安装以刷新链接。"
                    : "创建 ~/.local/bin/wuu 软链接。"}
              </span>
            </div>
            <button
              className="settings-button settings-button-primary"
              type="button"
              disabled={busy || sourceMissing}
              onClick={() => void runInstall(false)}
            >
              {busy ? "安装中…" : alreadyLinked ? "重新安装" : "安装"}
            </button>
          </div>
        ) : null}

        {needsOverwrite ? (
          <div className="settings-row" data-testid="cli-install-overwrite">
            <div className="settings-row-label">
              <span className="settings-row-label-title">已存在文件</span>
              <span className="settings-row-label-description">
                {installPath} 已存在，是否覆盖？
              </span>
            </div>
            <div>
              <button
                className="settings-button settings-button-primary"
                type="button"
                disabled={busy}
                onClick={() => void runInstall(true)}
              >
                覆盖
              </button>
              <button
                className="settings-button settings-button-ghost"
                type="button"
                disabled={busy}
                onClick={() => setNeedsOverwrite(false)}
              >
                取消
              </button>
            </div>
          </div>
        ) : null}

        {actionError ? <div className="settings-error">{actionError}</div> : null}
        {feedback && !actionError ? <div className="settings-saved">{feedback}</div> : null}

        {showPathHint ? (
          <div className="settings-row" data-testid="cli-install-path-hint">
            <div className="settings-row-label">
              <span className="settings-row-label-description">
                ~/.local/bin 不在 PATH 中。请把
                {" export PATH=\"$HOME/.local/bin:$PATH\" "}
                加入你的 shell 配置（如 ~/.zshrc 或 ~/.bashrc）后重开终端。
              </span>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
