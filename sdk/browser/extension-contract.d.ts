// Canonical browser contract for native extension contributions (NFX-C03/H-N).
//
// Consumers typecheck against this public definition or a byte-identical,
// locally pinned mirror recording its source commit, body hash and version.
// Type imports are erased: the host supplies request/stream through mount()
// and drives the returned lifecycle handle, with no consumer runtime host adapter.
// Consumer entries refuse an unexpected contractVersion before any effects.
//
// Display facts published by the host (route, presentation, observed
// repository) are never operation authority. Contributions must not inspect
// host ancestors, another mount, or browser cache events for authority.
//
// Guaranteed lifetimes: update() and suspend()/resume() always preserve the
// root and all retained resources (renderer/socket where used). Only retire()
// — authority loss or removal — closes contribution streams. The visible
// retained owner receives update() across ordinary navigation, never
// retire(); an owner sheltered while non-visible suspends and resumes with
// full retention.

/** Where the contribution is mounted: a native page slot or a retained panel. */
export type ContributionMode = 'page' | 'panel';

/**
 * Display-only presentation facts published by the host on every update.
 * `repositoryId` selects a display only; it never widens authority.
 */
export interface ContributionPresentation {
  mode: ContributionMode;
  /** Current host route path (no origin), e.g. "/workspace" or "/owner/repo". */
  route: string;
  /** Decoded route parameters as display strings. */
  params: Record<string, string>;
  /** Observed repository ID when a native repository route admits one. */
  repositoryId?: string;
  /** Whether the host currently shows this contribution. */
  visible: boolean;
}

/** Fetch subset the host forwards for a scoped contribution request. */
export interface ContributionRequestOptions {
  method?: string;
  headers?: HeadersInit;
  body?: BodyInit | null;
  signal?: AbortSignal | null;
}

/**
 * Options for opening a scoped contribution stream. The admitted stream path
 * accepts no query string and no subprotocols, so none are offered here.
 */
export interface ContributionStreamOptions {
  signal?: AbortSignal | null;
}

/**
 * Host-managed contribution stream. The socket stays connected across
 * update() and suspend()/resume(); the host closes it only on retire() or
 * authority loss.
 */
export interface ContributionStream {
  readonly socket: WebSocket;
  close(code?: number, reason?: string): void;
}

/**
 * Revalidation facts supplied to `resume` after the host rechecks the live
 * native session. A generation mismatch retires the mount instead.
 */
export interface ResumeValidation {
  sessionGeneration: string;
}

/**
 * Why the host is retiring a mount. Never a product execution command, and
 * never ordinary navigation: the visible shared owner receives update();
 * sheltered owners suspend and resume without retirement.
 */
export type RetireReason =
  | 'session-changed' |
  'authority-revoked' |
  'contribution-unavailable';

/**
 * Mount context supplied exactly once per root. The contribution receives
 * only its root and this public contract; the host chooses
 * visibility, placement, and connected lifetime.
 */
export interface MountContext {
  extensionId: string;
  pageId?: string;
  panelId?: string;
  /** Scoped API prefix ending in "/api/"; all requests stay under it. */
  apiBase: string;
  /** Scoped asset prefix; the entry module must load from under it. */
  assetBase: string;
  appSubUrl: string;
  /** Native session generation this mount was admitted under. */
  sessionGeneration: string;
  /**
   * Canonical browser contract version implemented by the host.
   * The host refuses a mismatched mount dataset. Consumer entries also
   * check their built expected version before any effects, binding the
   * consumer build to a compatible host runtime.
   */
  contractVersion: 1;
  /** Observed repository ID (display only, never authority). */
  repositoryId?: string;
  /**
   * Scoped fetch bound to apiBase and the mount's session generation.
   * Rejects cross-origin, credentialed, or encoded-separator targets
   * without sending them. In-flight requests abort when the owner
   * retires, disposes, or is invalidated; late responses after
   * terminal teardown are refused.
   */
  request: (relativePath: string, options?: ContributionRequestOptions) => Promise<Response>;
  /**
   * Scoped stream bound to apiBase and the mount's session generation.
   * The socket survives update() and suspend()/resume(); the host closes
   * it only on retire() or authority loss. No query string, subprotocols,
   * or handshake headers are sent: the admitted stream path rejects them
   * and admits the stream from the live cookie session.
   */
  stream: (relativePath: string, options?: ContributionStreamOptions) => ContributionStream;
}

/**
 * Optional lifecycle handle a mount may return. Every callback is
 * host-driven; the contribution never invents lifecycle transitions.
 */
export interface MountHandle {
  /** Release listeners, timers, and requests. Called at most once. */
  dispose?: () => void;
  /**
   * Apply ordered host presentation/context updates. Ordinary native
   * visibility, mode, and context changes arrive here; the root and all
   * retained resources (renderer/socket where used) stay connected.
   */
  update?: (presentation: ContributionPresentation) => void;
  /**
   * Concealment: hide private state and pause visible work while the owner
   * is sheltered non-visible across navigation or the host revalidates the
   * session. The root and all retained resources (renderer/socket where
   * used) stay connected; only retire() closes them.
   */
  suspend?: () => void;
  /**
   * Rebind visible work after shelter or session revalidation.
   * Suspend/resume never drops the socket.
   */
  resume?: (validation: ResumeValidation) => void;
  /**
   * Final teardown after authority loss or removal. The host closes
   * contribution streams first. Ordinary navigation never retires the
   * admitted shared owner; visible owners update and sheltered owners
   * suspend and resume with full retention.
   */
  retire?: (reason: RetireReason) => void;
}

/**
 * Entry modules export `mount`. It may be async. The return value selects
 * teardown behavior: a cleanup function, a `{dispose}` object, or a full
 * lifecycle handle. Anything else mounts without teardown.
 */
export type MountEntry = (
  root: HTMLElement,
  context: MountContext,
) => void | (() => void) | {dispose: () => void} | MountHandle | Promise<void | (() => void) | {dispose: () => void} | MountHandle>;
