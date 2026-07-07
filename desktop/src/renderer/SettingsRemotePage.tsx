/**
 * Remote-control settings page: enable the machine-global remote host,
 * show the pairing QR for a phone to scan, and manage paired devices.
 *
 * Self-contained and props-driven: all data and actions arrive from the
 * shell wiring (main-process RemoteHostManager over IPC), so the component
 * renders and tests in isolation. The markup mirrors the shared settings
 * primitives (section/card/row/switch) by class name.
 */
import { useEffect, useState, type ReactNode } from "react";
import QRCode from "qrcode";

export type RemoteDeviceView = {
  pub: string;
  fingerprint: string;
  name?: string;
  added_at: string;
};

export type RemoteStatusView = {
  fingerprint: string;
  host_name?: string;
  relay_url?: string;
  store: string;
  devices: RemoteDeviceView[];
};

export type SettingsRemotePageProps = {
  status: RemoteStatusView | null;
  statusError: string;
  hostRunning: boolean;
  pairUri: string | null;
  busy: boolean;
  onSaveRelay: (relayUrl: string) => void;
  onToggleHost: (enabled: boolean) => void;
  onOpenPairing: () => void;
  onRemoveDevice: (device: RemoteDeviceView) => void;
};

export function SettingsRemotePage({
  status,
  statusError,
  hostRunning,
  pairUri,
  busy,
  onSaveRelay,
  onToggleHost,
  onOpenPairing,
  onRemoveDevice
}: SettingsRemotePageProps): JSX.Element {
  const [relayDraft, setRelayDraft] = useState(status?.relay_url ?? "");
  useEffect(() => {
    setRelayDraft(status?.relay_url ?? "");
  }, [status?.relay_url]);
  const relayConfigured = Boolean(status?.relay_url);

  return (
    <div className="settings-page" data-testid="settings-remote-page">
      <header className="settings-page-header">
        <h1 className="settings-page-title">远程控制</h1>
        <p className="settings-page-description">在手机上查看、驱动、批准这台电脑上的 agent。中继只见密文,内容端到端加密。</p>
      </header>
      {statusError ? <div className="settings-error">{statusError}</div> : null}

      <RemoteSection title="中继与开关" description="手机与电脑经由中继互联;中继可自架(wuu relay)。">
        <div className="settings-card">
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const value = relayDraft.trim();
              if (value !== "") onSaveRelay(value);
            }}
          >
            <RemoteRow title="中继地址" description="ws[s]://主机:端口/v1/connect">
              <input
                className="settings-input"
                type="text"
                value={relayDraft}
                placeholder="ws://127.0.0.1:8787/v1/connect"
                onChange={(event) => setRelayDraft(event.target.value)}
                disabled={busy}
              />
            </RemoteRow>
            <div className="settings-card-footer">
              <button
                className="settings-button settings-button-primary"
                type="submit"
                disabled={busy || relayDraft.trim() === "" || relayDraft.trim() === (status?.relay_url ?? "")}
              >
                保存中继
              </button>
            </div>
          </form>
          <RemoteRow
            title="远程访问"
            description={
              relayConfigured
                ? hostRunning
                  ? "远程宿主运行中,已配对的手机可以连接"
                  : "开启后这台电脑向中继注册,等待手机连接"
                : "先配置中继地址"
            }
          >
            <button
              className="settings-switch"
              type="button"
              role="switch"
              aria-checked={hostRunning}
              disabled={busy || !relayConfigured}
              onClick={() => onToggleHost(!hostRunning)}
            >
              <span className="settings-switch-thumb" aria-hidden="true" />
              <span className="sr-only">{hostRunning ? "关闭远程访问" : "开启远程访问"}</span>
            </button>
          </RemoteRow>
        </div>
      </RemoteSection>

      <RemoteSection title="配对手机" description="配对码只出现在这块屏幕上,不经过中继——扫到码即证明人在电脑前。">
        <div className="settings-card">
          {pairUri ? (
            <div className="settings-remote-pairing" data-testid="remote-pair-panel">
              <PairQRCode uri={pairUri} />
              <p className="settings-remote-pair-hint">用手机 App 扫码,或手动粘贴以下配对链接:</p>
              <code className="settings-remote-pair-uri" data-testid="remote-pair-uri">
                {pairUri}
              </code>
            </div>
          ) : (
            <RemoteRow
              title="配对新手机"
              description={hostRunning ? "打开一个限时配对窗口并生成二维码" : "先开启远程访问"}
            >
              <button
                className="settings-button"
                type="button"
                disabled={busy || !hostRunning}
                onClick={onOpenPairing}
              >
                显示配对二维码
              </button>
            </RemoteRow>
          )}
        </div>
      </RemoteSection>

      <RemoteSection title="已配对设备" description="吊销后该手机将无法再连接这台电脑。">
        <div className="settings-card">
          {!status || status.devices.length === 0 ? (
            <div className="settings-empty">尚未配对任何手机</div>
          ) : (
            status.devices.map((device) => (
              <RemoteRow
                key={device.pub}
                title={device.name && device.name.trim() !== "" ? device.name : "未命名设备"}
                description={`${device.fingerprint} · 配对于 ${formatPairedAt(device.added_at)}`}
              >
                <button
                  className="settings-button settings-button-danger"
                  type="button"
                  disabled={busy}
                  onClick={() => onRemoveDevice(device)}
                >
                  吊销
                </button>
              </RemoteRow>
            ))
          )}
        </div>
      </RemoteSection>
    </div>
  );
}

/** Renders the pairing URI as an inline SVG QR code. SVG keeps the module
 *  free of canvas dependencies, so it renders identically in tests. */
export function PairQRCode({ uri }: { uri: string }): JSX.Element {
  const [svg, setSvg] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    setSvg("");
    setError("");
    QRCode.toString(uri, { type: "svg", errorCorrectionLevel: "M", margin: 1 })
      .then((rendered) => {
        if (!cancelled) setSvg(rendered);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [uri]);
  if (error) {
    return <div className="settings-error">二维码生成失败:{error}</div>;
  }
  return (
    <div
      className="settings-remote-qr"
      data-testid="remote-pair-qr"
      role="img"
      aria-label="配对二维码"
      // The SVG comes from the qrcode encoder over our own URI, not from
      // remote input.
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}

function formatPairedAt(addedAt: string): string {
  const date = new Date(addedAt);
  if (Number.isNaN(date.getTime())) {
    return addedAt;
  }
  return date.toISOString().slice(0, 10);
}

/* Local copies of the settings primitives' markup: the originals live inside
 * SettingsView.tsx unexported; the wiring commit can consolidate them. */

function RemoteSection({
  title,
  description,
  children
}: {
  title: string;
  description: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="settings-section">
      <header className="settings-section-header">
        <h2 className="settings-section-title">{title}</h2>
        <p className="settings-section-description">{description}</p>
      </header>
      {children}
    </section>
  );
}

function RemoteRow({
  title,
  description,
  children
}: {
  title: string;
  description?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="settings-row">
      <div className="settings-row-label">
        <span className="settings-row-label-title">{title}</span>
        {description ? <span className="settings-row-label-description">{description}</span> : null}
      </div>
      <div className="settings-row-control">{children}</div>
    </div>
  );
}
