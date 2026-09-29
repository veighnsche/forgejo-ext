function isAuthPath(pathname) {
  return /(?:^|\/)user\/(?:login|logout|sign_up|forgot_password|forget_password|reset_password|recover_account|activate|activate_email|two_factor|webauthn|oauth2|openid|link_account|link_account_signin|link_account_signup)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)user\/settings\/(?:change_password|security|applications|keys)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)(?:install|login\/oauth|login\/openid|oauth2|openid)(?:\/|$)/.test(pathname);
}

function isWorkspacePath(pathname) {
  return pathname.endsWith('/-/extensions/workspace') || pathname.includes('/-/extensions/workspace/');
}

function hasCredentialQuery(url) {
  return Array.from(url.searchParams.keys()).some((key) => /^(?:token|password|secret|client_secret|authorization|auth|code|credential|session|api_key|private_key|.*_token)$/i.test(key));
}

/** Classify a browser destination before allowing it into the workspace frame. */
export function classifyWorkspaceNavigation(input, base, origin, appSubPath = '') {
  let url;
  try {
    url = new URL(input, base);
  } catch {
    return {kind: 'reject'};
  }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return {kind: 'reject'};
  if (/%2f|%5c/i.test(url.pathname)) return {kind: 'reject'};
  if (appSubPath && !url.pathname.startsWith(`${appSubPath}/`)) return {kind: 'top', url: url.href};
  if (url.origin !== origin || isAuthPath(url.pathname) || hasCredentialQuery(url)) {
    return {kind: 'top', url: url.href};
  }
  if (isWorkspacePath(url.pathname)) return {kind: 'reject'};
  return {kind: 'frame', url: url.href, path: `${url.pathname}${url.search}${url.hash}`};
}

export function workspacePath(input, base, origin, appSubPath = '') {
  if (!input?.startsWith('/') || input.startsWith('//')) return null;
  const result = classifyWorkspaceNavigation(input, base, origin, appSubPath);
  return result.kind === 'frame' ? result.path : null;
}

const importExtension = (url) => import(/* webpackIgnore: true */ url);
const noop = () => {};
const sessionCheckPath = (appSubPath) => `${appSubPath}/-/extensions/workspace?session_check=1`;

function liveSessionMatches(url, generation, fetcher) {
  return Promise.resolve().then(() => fetcher(url, {credentials: 'same-origin', cache: 'no-store', redirect: 'manual'}))
    .then((response) => response.status === 204 && response.headers.get('X-Extension-Session-Generation') === generation)
    .catch(() => false);
}

export async function mountExtension(root, importer = importExtension) {
  const {extensionEntry, extensionId, extensionPageId, extensionPanelId, extensionApiBase, extensionAssetBase, extensionSessionGeneration} = root.dataset;
  const entry = classifyWorkspaceNavigation(extensionEntry, window.location.href, window.location.origin);
  const assets = classifyWorkspaceNavigation(extensionAssetBase, window.location.href, window.location.origin);
  const api = classifyWorkspaceNavigation(extensionApiBase, window.location.href, window.location.origin);
  if (entry.kind !== 'frame' || assets.kind !== 'frame' || api.kind !== 'frame' || !entry.url.startsWith(assets.url) ||
      !api.url.endsWith('/api/') || !extensionSessionGeneration) {
    root.textContent = 'Extension mount is invalid.';
    return () => {};
  }
  const request = (relativePath, options = {}) => {
    const target = new URL(relativePath, api.url);
    if (target.origin !== window.location.origin || target.username || target.password ||
        !target.href.startsWith(api.url) || /%2f|%5c/i.test(target.pathname)) {
      throw new TypeError('Extension API URL is invalid');
    }
    const headers = new Headers(options.headers);
    headers.set('X-Extension-Session-Generation', extensionSessionGeneration);
    return window.fetch(target.href, {...options, credentials: 'same-origin', redirect: 'error', cache: 'no-store', headers});
  };
  try {
    const module = await importer(entry.url);
    if (typeof module.mount !== 'function') throw new TypeError('Extension entry must export mount');
    const result = await module.mount(root, {
      extensionId,
      pageId: extensionPageId,
      panelId: extensionPanelId,
      apiBase: extensionApiBase,
      assetBase: extensionAssetBase,
      sessionGeneration: extensionSessionGeneration,
      request,
    });
    return typeof result === 'function' ? result : typeof result?.dispose === 'function' ? () => result.dispose() : noop;
  } catch (error) {
    console.error('Could not load extension', error);
    root.textContent = 'Could not load this extension.';
    return noop;
  }
}

export function initExtensionPages(importer = importExtension, fetcher = (...args) => window.fetch(...args)) {
  if (initPreferredExtensionWorkspace()) return;
  const roots = document.querySelectorAll('[data-extension-page], [data-extension-panel]');
  if (!roots.length) return;
  const pageRoots = Array.from(roots).filter((root) => root.hasAttribute('data-extension-page'));
  const disposers = [];
  let leaving = false;
  for (const root of roots) mountExtension(root, importer).then((dispose) => {
    if (leaving) dispose();
    else disposers.push(dispose);
  });
  const disposeRoots = () => {
    if (leaving) return;
    leaving = true;
    for (const dispose of disposers) dispose();
    window.removeEventListener('extension-workspace-invalidated', disposeRoots);
  };
  window.addEventListener('pagehide', (event) => { if (!event.persisted) disposeRoots(); });
  window.addEventListener('extension-workspace-invalidated', disposeRoots);
  if (!pageRoots.length) return;
  const generation = pageRoots[0].dataset.extensionSessionGeneration;
  const checkURL = new URL(sessionCheckPath(window.config.appSubUrl), window.location.origin).href;
  let checkVersion = 0;
  let pendingCheck;
  let invalidated = false;
  const invalidate = () => {
    if (invalidated) return;
    invalidated = true;
    for (const root of pageRoots) {
      root.replaceChildren();
      root.style.display = '';
      root.textContent = 'Your session changed. Reload Forgejo to continue.';
    }
    disposeRoots();
  };
  const checkSession = () => {
    if (invalidated || pendingCheck) return;
    for (const root of pageRoots) root.style.display = 'none';
    const version = ++checkVersion;
    pendingCheck = liveSessionMatches(checkURL, generation, fetcher).then((matches) => {
      if (version !== checkVersion) return;
      if (!matches) invalidate();
      else for (const root of pageRoots) root.style.display = '';
    }).finally(() => { if (version === checkVersion) pendingCheck = null; });
  };
  const onPageShow = (event) => { if (event.persisted) checkSession(); };
  const onVisibilityChange = () => { if (document.visibilityState === 'visible') checkSession(); };
  const onPageHide = (event) => {
    checkVersion++;
    pendingCheck = null;
    for (const root of pageRoots) root.style.display = 'none';
    if (event.persisted) return;
    window.removeEventListener('pagehide', onPageHide);
    window.removeEventListener('pageshow', onPageShow);
    window.removeEventListener('focus', checkSession);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
  window.addEventListener('pagehide', onPageHide);
  window.addEventListener('pageshow', onPageShow);
  window.addEventListener('focus', checkSession);
  document.addEventListener('visibilitychange', onVisibilityChange);
  if (!generation) invalidate();
}

/** The native page marks this only after the server finds an enabled preferred workspace. */
export function initPreferredExtensionWorkspace() {
  if (window.top !== window) return;
  const hostPath = document.documentElement.dataset.extensionPreferredWorkspace;
  if (!hostPath) return;
  let host;
  try {
    host = new URL(hostPath, window.location.href);
  } catch {
    return;
  }
  if (host.origin !== window.location.origin || !host.pathname.endsWith('/-/extensions/workspace')) return;
  const appSubPath = host.pathname.replace(/\/-\/extensions\/workspace$/, '');
  const decision = classifyWorkspaceNavigation(window.location.href, window.location.href, window.location.origin, appSubPath);
  if (decision.kind !== 'frame') return;
  host.searchParams.set('path', decision.path);
  window.location.replace(host.href);
  return true;
}

export function initExtensionWorkspace(fetcher = (...args) => window.fetch(...args)) {
  const workspace = document.querySelector('[data-extension-workspace]');
  if (!workspace) return;
  const frame = workspace.querySelector('[data-extension-workspace-frame]');
  const location = workspace.querySelector('[data-extension-workspace-location]');
  const toggle = workspace.querySelector('[data-extension-workspace-toggle]');
  const base = window.location.href;
  const origin = window.location.origin;
  const appSubPath = window.location.pathname.replace(/\/-\/extensions\/workspace$/, '');
  const generation = workspace.dataset.workspaceSessionGeneration;
  let invalidated = false;
  let pendingCheck;
  let checkVersion = 0;

  function invalidate() {
    if (invalidated) return;
    invalidated = true;
    frame.src = 'about:blank';
    workspace.replaceChildren();
    workspace.style.display = '';
    workspace.textContent = 'Your session changed. Reload Forgejo to continue.';
    window.dispatchEvent(new Event('extension-workspace-invalidated'));
  }

  function checkSession() {
    if (invalidated || pendingCheck) return pendingCheck;
    workspace.style.display = 'none';
    const version = ++checkVersion;
    const endpoint = new URL(sessionCheckPath(appSubPath), origin);
    pendingCheck = liveSessionMatches(endpoint.href, generation, fetcher)
      .then((matches) => {
        if (version !== checkVersion) return;
        if (!matches) {
          invalidate();
          return;
        }
        workspace.style.display = '';
      })
      .finally(() => {
        if (version === checkVersion) pendingCheck = null;
      });
    return pendingCheck;
  }

  if (!generation) {
    invalidate();
    return;
  }
  // A cached document stays hidden until the current native session is checked.
  const onPageShow = (event) => { if (event.persisted) checkSession(); };
  const onVisibilityChange = () => { if (document.visibilityState === 'visible') checkSession(); };
  window.addEventListener('pageshow', onPageShow);
  window.addEventListener('focus', checkSession);
  document.addEventListener('visibilitychange', onVisibilityChange);
  const initial = workspacePath(workspace.dataset.workspacePath, base, origin, appSubPath) || `${appSubPath}/`;
  let current = initial;
  let activeDocument;
  let activeWindow;

  toggle?.addEventListener('click', () => {
    const focused = workspace.classList.toggle('focus-view');
    toggle.setAttribute('aria-expanded', String(!focused));
    toggle.textContent = focused ? 'Show panels' : 'Focus browsing';
    if (focused) frame.focus();
  });

  function updateHistory(path) {
    const address = new URL(window.location.href);
    address.searchParams.set('path', path);
    const state = window.history.state;
    window.history.replaceState({...((state && typeof state === 'object') ? state : {}), extensionWorkspacePath: path}, '', address);
  }

  function onFrameClick(event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = event.composedPath().find((element) => element?.localName === 'a' && 'href' in element);
    if (!link || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
    const decision = classifyWorkspaceNavigation(link.href, frame.contentWindow.location.href, origin, appSubPath);
    if (decision.kind === 'frame') return;
    event.preventDefault();
    if (decision.kind === 'top') window.location.assign(decision.url);
  }

  function onFrameSubmit(event) {
    if (event.defaultPrevented) return;
    const form = event.target;
    if (form?.localName !== 'form' || (form.target && form.target !== '_self')) return;
    const decision = classifyWorkspaceNavigation(form.action, frame.contentWindow.location.href, origin, appSubPath);
    if (decision.kind === 'top') form.target = '_top';
    if (decision.kind === 'reject') event.preventDefault();
  }

  function onFrameNavigation() {
    if (invalidated) return;
    let decision;
    try {
      decision = classifyWorkspaceNavigation(frame.contentWindow.location.href, base, origin, appSubPath);
    } catch {
      frame.src = current;
      return;
    }
    if (decision.kind === 'top') {
      window.location.assign(decision.url);
      return;
    }
    if (decision.kind !== 'frame') {
      frame.src = current;
      return;
    }
    location.textContent = decision.path;
    if (decision.path !== current) {
      current = decision.path;
      updateHistory(current);
    }
  }

  frame.addEventListener('load', () => {
    if (invalidated) return;
    try {
      activeDocument?.removeEventListener('click', onFrameClick);
      activeDocument?.removeEventListener('submit', onFrameSubmit);
      activeWindow?.removeEventListener('hashchange', onFrameNavigation);
      activeWindow?.removeEventListener('popstate', onFrameNavigation);
      activeDocument = frame.contentDocument;
      activeWindow = frame.contentWindow;
      activeDocument.addEventListener('click', onFrameClick);
      activeDocument.addEventListener('submit', onFrameSubmit);
      activeWindow.addEventListener('hashchange', onFrameNavigation);
      activeWindow.addEventListener('popstate', onFrameNavigation);
    } catch {
      frame.src = current;
      return;
    }
    onFrameNavigation();
  });
  const onPopState = () => {
    if (invalidated) return;
    const path = workspacePath(new URL(window.location.href).searchParams.get('path'), base, origin, appSubPath);
    if (!path || path === current) return;
    current = path;
    frame.src = path;
  };
  window.addEventListener('popstate', onPopState);
  const onPageHide = (event) => {
    checkVersion++;
    pendingCheck = null;
    workspace.style.display = 'none';
    if (event.persisted) return;
    window.removeEventListener('pagehide', onPageHide);
    window.removeEventListener('pageshow', onPageShow);
    window.removeEventListener('focus', checkSession);
    window.removeEventListener('popstate', onPopState);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
  window.addEventListener('pagehide', onPageHide);
  updateHistory(initial);
  location.textContent = initial;
  frame.src = initial;
}
