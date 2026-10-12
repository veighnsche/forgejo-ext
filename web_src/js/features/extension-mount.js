// Retained contribution owner for the native extension host (NFX-C02/H-0).
//
// C-owned implementation; A02 integrates it into the native transition seam.
// This module owns mount validation, the contribution lifecycle
// (update/suspend/resume/retire) and scoped transport. A's presentation layer
// owns root visibility/text and the native transition; B's lifecycle hooks own
// native feature init/teardown. The dataset validators intentionally mirror
// extensions.js until A02 unifies them; C does not edit that A-owned file.

function validAppSubURL(value) {
  return typeof value === 'string' && (value === '' ||
    /^\/[^/\\?#\s]+(?:\/[^/\\?#\s]+)*$/.test(value) &&
    !/%2f|%5c/i.test(value) && !value.split('/').some((part) => part === '.' || part === '..'));
}

function nativeMountURL(input, appSubURL) {
  if (!validAppSubURL(appSubURL) || typeof input !== 'string') return null;
  try {
    const url = new URL(input, window.location.href);
    return url.origin === window.location.origin && !url.username && !url.password && !url.search && !url.hash &&
      !/%2f|%5c/i.test(url.pathname) && url.pathname.startsWith(`${appSubURL}/`) ? url : null;
  } catch {
    return null;
  }
}

function nativeAPIPath(pathname, appSubURL, suffix) {
  if (!pathname.endsWith(suffix)) return false;
  const prefix = pathname.slice(0, -suffix.length);
  if (!prefix.startsWith(appSubURL)) return false;
  const scope = prefix.slice(appSubURL.length);
  return scope === '' || scope === '/user/settings' || scope === '/admin' || /^\/[^/]+\/[^/]+$/.test(scope);
}

function scopedTarget(relativePath, apiHref) {
  const target = new URL(relativePath, apiHref);
  if (target.origin !== window.location.origin || target.username || target.password ||
      !target.href.startsWith(apiHref) || /%2f|%5c/i.test(target.pathname)) {
    return null;
  }
  return target;
}

/** Validate a contribution root's dataset without importing or fetching. */
export function parseMountDataset(root) {
  const {appSubUrl, extensionEntry, extensionId, extensionPageId, extensionPanelId, extensionApiBase, extensionAssetBase, extensionSessionGeneration, extensionRepositoryId} = root.dataset;
  const mode = extensionPageId ? 'page' : 'panel';
  const contributionId = extensionPageId || extensionPanelId;
  if (!extensionId || !contributionId || !extensionSessionGeneration) return {ok: false};
  const entry = nativeMountURL(extensionEntry, appSubUrl);
  const assets = nativeMountURL(extensionAssetBase, appSubUrl);
  const api = nativeMountURL(extensionApiBase, appSubUrl);
  const apiSuffix = `/-/native/${mode}s/${extensionId}/${contributionId}/api/`;
  if (!entry || !assets || !api || assets.pathname !== `${appSubUrl}/-/native/assets/${extensionId}/` ||
      !entry.href.startsWith(assets.href) || !nativeAPIPath(api.pathname, appSubUrl, apiSuffix)) {
    return {ok: false};
  }
  return {
    ok: true, mode, contributionId,
    entryHref: entry.href,
    apiHref: api.href,
    assetHref: assets.href,
    extensionId, appSubUrl,
    sessionGeneration: extensionSessionGeneration,
    repositoryId: extensionRepositoryId,
  };
}

/** Scoped fetch bound to the contribution apiBase and session generation. */
export function createScopedRequest(apiHref, generation, fetcher) {
  return (relativePath, options = {}) => {
    const target = scopedTarget(relativePath, apiHref);
    if (!target) throw new TypeError('Product API URL is invalid');
    const headers = new Headers(options.headers);
    headers.set('X-Extension-Session-Generation', generation);
    return fetcher(target.href, {...options, credentials: 'same-origin', redirect: 'error', cache: 'no-store', headers});
  };
}

/**
 * Scoped stream bound to the contribution apiBase. The browser cannot set
 * handshake headers, so the server admits the stream from the live cookie
 * session; no query string or subprotocols are sent (the server rejects them).
 */
export function createScopedStream(apiHref, websocketClass) {
  if (typeof websocketClass !== 'function') throw new TypeError('Contribution streams are unavailable');
  const wsBase = new URL(apiHref);
  wsBase.protocol = wsBase.protocol === 'https:' ? 'wss:' : 'ws:';
  return (relativePath, options = {}) => {
    const target = scopedTarget(relativePath, apiHref);
    if (!target || target.search || target.hash) throw new TypeError('Product stream URL is invalid');
    const socket = new websocketClass(`${wsBase.protocol}//${target.host}${target.pathname}`);
    if (options.signal) {
      if (options.signal.aborted) socket.close();
      else options.signal.addEventListener('abort', () => socket.close(), {once: true});
    }
    return socket;
  };
}

const defaultImporter = (url) => import(/* webpackIgnore: true */ url);

function normalizeHandle(result) {
  if (typeof result === 'function') return {dispose: result};
  if (result && typeof result === 'object') {
    const handle = {};
    for (const key of ['dispose', 'update', 'suspend', 'resume', 'retire']) {
      if (typeof result[key] === 'function') handle[key] = (...args) => result[key](...args);
    }
    return handle;
  }
  return {};
}

/**
 * Mount one retained contribution owner. Returns the owner handle, or null
 * when the root refuses to mount (no import or fetch is attempted then).
 * The host drives update/suspend/resume/retire; A02 calls update alongside
 * native page-feature init and retire alongside native page-feature teardown.
 */
export async function mountContribution(root, hooks = {}) {
  const {importer = defaultImporter, fetcher = (...args) => window.fetch(...args), websocketClass = globalThis.WebSocket} = hooks;
  const parsed = parseMountDataset(root);
  if (!parsed.ok) {
    root.textContent = 'This workspace view is unavailable.';
    return null;
  }
  const sockets = new Set();
  let state = 'active';
  const rawRequest = createScopedRequest(parsed.apiHref, parsed.sessionGeneration, fetcher);
  const request = (...args) => {
    if (state === 'retired' || state === 'disposed') throw new TypeError('Contribution transport is retired');
    return rawRequest(...args);
  };
  const stream = (...args) => {
    if (state === 'retired' || state === 'disposed') throw new TypeError('Contribution transport is retired');
    const socket = createScopedStream(parsed.apiHref, websocketClass)(...args);
    sockets.add(socket);
    return socket;
  };
  const context = {
    extensionId: parsed.extensionId,
    pageId: parsed.mode === 'page' ? parsed.contributionId : undefined,
    panelId: parsed.mode === 'panel' ? parsed.contributionId : undefined,
    apiBase: parsed.apiHref,
    assetBase: parsed.assetHref,
    appSubUrl: parsed.appSubUrl,
    sessionGeneration: parsed.sessionGeneration,
    repositoryId: parsed.repositoryId,
    request,
    stream,
  };
  let module;
  try {
    module = await importer(parsed.entryHref);
  } catch (error) {
    console.error('Could not load workspace view', error);
    root.textContent = 'Could not load this workspace view.';
    return null;
  }
  if (typeof module.mount !== 'function') {
    root.textContent = 'Could not load this workspace view.';
    return null;
  }
  let handle = {};
  try {
    handle = normalizeHandle(await module.mount(root, context));
  } catch (error) {
    console.error('Could not load workspace view', error);
    root.textContent = 'Could not load this workspace view.';
    return null;
  }
  const closeSockets = () => {
    for (const socket of sockets) {
      try {
        socket.close();
      } catch {
        // A closing socket never blocks teardown.
      }
    }
    sockets.clear();
  };
  const disposeHandle = () => {
    // Retire already disposes the handle; both states are terminal.
    if (state === 'disposed' || state === 'retired') return;
    state = 'disposed';
    closeSockets();
    handle.dispose?.();
  };
  return {
    root,
    context,
    update: (presentation) => {
      if (state !== 'active' && state !== 'suspended') return;
      handle.update?.(presentation);
    },
    suspend: () => {
      // Authority concealment only: sockets stay connected across
      // suspend/resume. Only retire() closes them.
      if (state !== 'active') return;
      state = 'suspended';
      handle.suspend?.();
    },
    resume: (validation) => {
      if (state !== 'suspended') return;
      state = 'active';
      handle.resume?.(validation);
    },
    retire: (reason) => {
      if (state === 'retired' || state === 'disposed') return;
      state = 'retired';
      closeSockets();
      try {
        handle.retire?.(reason);
      } finally {
        handle.dispose?.();
      }
    },
    dispose: disposeHandle,
  };
}
