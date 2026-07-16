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
import { useI18n } from "./i18n";

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
  const { t, formatDate } = useI18n();
  const [relayDraft, setRelayDraft] = useState(status?.relay_url ?? "");
  useEffect(() => {
    setRelayDraft(status?.relay_url ?? "");
  }, [status?.relay_url]);
  const relayConfigured = Boolean(status?.relay_url);

  return (
    <div data-testid="settings-remote-page">
      {statusError ? <div className="settings-error">{statusError}</div> : null}

      <RemoteSection title={t("remote.relaySection")} description={t("remote.relaySectionDescription")}>
        <div className="settings-card">
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const value = relayDraft.trim();
              if (value !== "") onSaveRelay(value);
            }}
          >
            <RemoteRow
              title={t("remote.relayAddress")}
              description={t("remote.relayFormat")}
              block
            >
              <input
                className="settings-input"
                type="text"
                value={relayDraft}
                placeholder="ws://127.0.0.1:8787/v1/connect"
                onChange={(event) => setRelayDraft(event.target.value)}
                disabled={busy}
              />
              <button
                className="settings-button settings-button-primary"
                type="submit"
                disabled={busy || relayDraft.trim() === "" || relayDraft.trim() === (status?.relay_url ?? "")}
              >
                {t("remote.saveRelay")}
              </button>
            </RemoteRow>
          </form>
          <RemoteRow
            title={t("remote.access")}
            description={
              relayConfigured
                ? hostRunning
                  ? t("remote.hostRunning")
                  : t("remote.hostStopped")
                : t("remote.configureRelayFirst")
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
              <span className="sr-only">{hostRunning ? t("remote.disableAccess") : t("remote.enableAccess")}</span>
            </button>
          </RemoteRow>
        </div>
      </RemoteSection>

      <RemoteSection title={t("remote.pairSection")} description={t("remote.pairSectionDescription")}>
        <div className="settings-card">
          {pairUri ? (
            <div className="settings-remote-pairing" data-testid="remote-pair-panel">
              <PairQRCode uri={pairUri} />
              <p className="settings-remote-pair-hint">{t("remote.pairHint")}</p>
              <code className="settings-remote-pair-uri" data-testid="remote-pair-uri">
                {pairUri}
              </code>
            </div>
          ) : (
            <RemoteRow
              title={t("remote.pairNewPhone")}
              description={hostRunning ? t("remote.openPairWindow") : t("remote.enableAccessFirst")}
            >
              <button
                className="settings-button"
                type="button"
                disabled={busy || !hostRunning}
                onClick={onOpenPairing}
              >
                {t("remote.showPairQr")}
              </button>
            </RemoteRow>
          )}
        </div>
      </RemoteSection>

      <RemoteSection title={t("remote.devicesSection")} description={t("remote.devicesSectionDescription")}>
        <div className="settings-card">
          {!status || status.devices.length === 0 ? (
            <div className="settings-empty">{t("remote.noDevices")}</div>
          ) : (
            status.devices.map((device) => (
              <RemoteRow
                key={device.pub}
                title={device.name && device.name.trim() !== "" ? device.name : t("remote.unnamedDevice")}
                description={t("remote.pairedAt", { fingerprint: device.fingerprint, date: formatPairedAt(device.added_at, formatDate) })}
              >
                <button
                  className="settings-button settings-button-danger"
                  type="button"
                  disabled={busy}
                  onClick={() => onRemoveDevice(device)}
                >
                  {t("remote.revoke")}
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
  const { t } = useI18n();
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
    return <div className="settings-error">{t("remote.qrFailed", { error })}</div>;
  }
  return (
    <div
      className="settings-remote-qr"
      data-testid="remote-pair-qr"
      role="img"
      aria-label={t("remote.pairQr")}
      // The SVG comes from the qrcode encoder over our own URI, not from
      // remote input.
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}

function formatPairedAt(
  addedAt: string,
  formatter: (value: Date | number | string, options?: Intl.DateTimeFormatOptions) => string,
): string {
  const date = new Date(addedAt);
  if (Number.isNaN(date.getTime())) {
    return addedAt;
  }
  return formatter(date, { year: "numeric", month: "short", day: "numeric" });
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
  block = false,
  children
}: {
  title: string;
  description?: string;
  block?: boolean;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="settings-row">
      <div className="settings-row-label">
        <span className="settings-row-label-title">{title}</span>
        {description ? <span className="settings-row-label-description">{description}</span> : null}
      </div>
      <div className={block ? "settings-row-control-block" : "settings-row-control"}>{children}</div>
    </div>
  );
}
