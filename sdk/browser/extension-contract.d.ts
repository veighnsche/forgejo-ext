// Prototype browser contract for native extension contributions (NFX-C01/H-0).
//
// Canonical declarations owned by Fountain lane C. Consumers vendor this file
// by copy; it is the only browser-contract surface a contribution may rely on.
// Signatures stay narrow until the A03 native capability proof finalizes them.
//
// Display facts published by the host (route, presentation, observed
// repository) are never operation authority. Contributions must not inspect
// host ancestors, another mount, or browser cache events for authority.

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

/** Options for opening a scoped contribution stream. */
export interface ContributionStreamOptions {
  protocols?: string | string[];
  signal?: AbortSignal | null;
}

/**
 * Host-managed contribution stream. The host closes the socket when the
 * contribution suspends with concealed state, retires, or loses authority.
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

/** Why the host is retiring a mount. Never a product execution command. */
export type RetireReason =
  | 'session-changed'
  | 'authority-revoked'
  | 'contribution-unavailable'
  | 'host-navigation';

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
  /** Observed repository ID (display only, never authority). */
  repositoryId?: string;
  /**
   * Scoped fetch bound to apiBase and the mount's session generation.
   * Rejects cross-origin, credentialed, or encoded-separator targets
   * without sending them.
   */
  request: (relativePath: string, options?: ContributionRequestOptions) => Promise<Response>;
  /**
   * Scoped stream bound to apiBase and the mount's session generation.
   * The host closes it on suspension with concealed state, retirement,
   * or authority loss.
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
  /** Apply ordered host presentation/context updates. */
  update?: (presentation: ContributionPresentation) => void;
  /**
   * Conceal private state and pause visible work. Retained resources
   * (including renderer/socket where used) stay connected.
   */
  suspend?: () => void;
  /** Rebind visible work after the host revalidates the session. */
  resume?: (validation: ResumeValidation) => void;
  /** Final teardown after authority loss or removal. Release everything. */
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
) => void | (() => void) | { dispose: () => void } | MountHandle | Promise<void | (() => void) | { dispose: () => void } | MountHandle>;
