import mascotFace from "./assets/mascot-face.png";

export function RuntimeLoading({
  status,
  pinned = false,
  onExitPreview
}: {
  status: string;
  pinned?: boolean;
  onExitPreview?: () => void;
}): JSX.Element {
  const isStarting = pinned || status === "connecting" || status === "opening";
  return (
    <div className="project-empty-pane">
      {isStarting ? (
        <div className="wuu-launch" role="status" aria-label={pinned ? "wuu 启动动画预览" : "wuu 正在启动"}>
          <div className="wuu-launch-mark" aria-hidden="true">
            <span>w</span>
            <span>u</span>
            <span>u</span>
          </div>
          <div className="wuu-launch-rail" aria-hidden="true" />
          {pinned && onExitPreview ? (
            <button className="wuu-launch-exit" type="button" onClick={onExitPreview}>
              退出预览
            </button>
          ) : null}
        </div>
      ) : (
        <div className="project-empty-content">
          <h2>{status}</h2>
        </div>
      )}
    </div>
  );
}

export function ViewSwitchLoading(): JSX.Element {
  return (
    <div className="view-switch-loading" role="status" aria-label="正在切换">
      <div className="wuu-launch-mark view-switch-mark" aria-hidden="true">
        <span>w</span>
        <span>u</span>
        <span>u</span>
      </div>
      <div className="wuu-launch-rail view-switch-rail" aria-hidden="true" />
    </div>
  );
}

export function EmptyConversationHome({
  title,
  belowTitle,
  children
}: {
  title: string;
  // Optional element rendered directly under the title in the same
  // grid cell so it can sit a few pixels below the greeting without
  // inheriting the very large row-gap reserved for the hero composer.
  belowTitle?: JSX.Element;
  children: JSX.Element;
}): JSX.Element {
  return (
    <section className="empty-home">
      <div className="empty-home-inner session-flow">
        <div className="empty-home-header">
          {/* Skin-gated: visible only when the flame skin sets
              --empty-home-mascot-display (see turns.css). */}
          <img
            className="empty-home-mascot"
            src={mascotFace}
            alt=""
            aria-hidden="true"
            draggable={false}
          />
          <h2>{title}</h2>
          {belowTitle ?? null}
        </div>
        {children}
      </div>
    </section>
  );
}
