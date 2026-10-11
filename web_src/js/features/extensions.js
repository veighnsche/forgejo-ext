function isAuthPath(pathname) {
  return /(?:^|\/)user\/(?:login|logout|sign_up|forgot_password|forget_password|reset_password|recover_account|activate|activate_email|two_factor|webauthn|oauth2|openid|link_account|link_account_signin|link_account_signup)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)user\/settings\/(?:change_password|security|applications|keys)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)(?:install|login\/oauth|login\/openid|oauth2|openid)(?:\/|$)/.test(pathname);
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
  if (/(?:^|\/)-\/native(?:\/|$)/.test(url.pathname)) return {kind: 'reject'};
  return {kind: 'frame', url: url.href, path: `${url.pathname}${url.search}${url.hash}`};
}

export function workspacePath(input, base, origin, appSubPath = '') {
  if (!input?.startsWith('/') || input.startsWith('//')) return null;
  const result = classifyWorkspaceNavigation(input, base, origin, appSubPath);
  return result.kind === 'frame' ? result.path : null;
}

/** Report whether a pathname is the workspace root or an explicit workspace alias. */
export function workspaceDirectPath(pathname) {
  if (pathname.endsWith('/workspace') || pathname.endsWith('/workspace/new')) return true;
  const marker = '/workspace/requests/';
  const index = pathname.lastIndexOf(marker);
  if (index < 0) return false;
  const segment = pathname.slice(index + marker.length);
  return segment !== '' && !segment.includes('/');
}

const importExtension = (url) => import(/* webpackIgnore: true */ url);
const noop = () => {};
const sessionCheckPath = (appSubPath) => `${appSubPath}/-/native/session`;

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

function liveSessionMatches(url, generation, fetcher) {
  return Promise.resolve().then(() => fetcher(url, {credentials: 'same-origin', cache: 'no-store', redirect: 'manual'}))
    .then((response) => response.status === 204 && response.headers.get('X-Extension-Session-Generation') === generation)
    .catch(() => false);
}

export async function mountExtension(root, importer = importExtension) {
  const {appSubUrl, extensionEntry, extensionId, extensionPageId, extensionPanelId, extensionApiBase, extensionAssetBase, extensionSessionGeneration, extensionRepositoryId} = root.dataset;
  const entry = nativeMountURL(extensionEntry, appSubUrl);
  const assets = nativeMountURL(extensionAssetBase, appSubUrl);
  const api = nativeMountURL(extensionApiBase, appSubUrl);
  const contribution = extensionPageId ? 'pages' : 'panels';
  const contributionID = extensionPageId || extensionPanelId;
  const apiSuffix = `/-/native/${contribution}/${extensionId}/${contributionID}/api/`;
  if (!entry || !assets || !api || assets.pathname !== `${appSubUrl}/-/native/assets/${extensionId}/` ||
      !entry.href.startsWith(assets.href) || !nativeAPIPath(api.pathname, appSubUrl, apiSuffix) ||
      !extensionSessionGeneration) {
    root.textContent = 'This workspace view is unavailable.';
    return () => {};
  }
  const request = (relativePath, options = {}) => {
    const target = new URL(relativePath, api.href);
    if (target.origin !== window.location.origin || target.username || target.password ||
        !target.href.startsWith(api.href) || /%2f|%5c/i.test(target.pathname)) {
      throw new TypeError('Product API URL is invalid');
    }
    const headers = new Headers(options.headers);
    headers.set('X-Extension-Session-Generation', extensionSessionGeneration);
    return window.fetch(target.href, {...options, credentials: 'same-origin', redirect: 'error', cache: 'no-store', headers});
  };
  try {
    const module = await importer(entry.href);
    if (typeof module.mount !== 'function') throw new TypeError('Workspace view must export mount');
    const result = await module.mount(root, {
      extensionId,
      pageId: extensionPageId,
      panelId: extensionPanelId,
      appSubUrl,
      repositoryId: extensionRepositoryId,
      apiBase: extensionApiBase,
      assetBase: extensionAssetBase,
      sessionGeneration: extensionSessionGeneration,
      request,
    });
    return typeof result === 'function' ? result : typeof result?.dispose === 'function' ? () => result.dispose() : noop;
  } catch (error) {
    console.error('Could not load workspace view', error);
    root.textContent = 'Could not load this workspace view.';
    return noop;
  }
}

export function initExtensionPages(importer = importExtension, fetcher = (...args) => window.fetch(...args)) {
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
  const appSubURL = pageRoots[0].dataset.appSubUrl;
  const checkURL = new URL(sessionCheckPath(appSubURL), window.location.origin).href;
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
  if (!generation || !validAppSubURL(appSubURL)) invalidate();
}

export function initExtensionWorkspace(fetcher = (...args) => window.fetch(...args)) {
  const workspace = document.querySelector('[data-extension-workspace]');
  if (!workspace) return;
  let frame = workspace.querySelector('[data-extension-workspace-frame]');
  const navbar = document.getElementById('navbar');
  const footer = document.querySelector('footer');
  let directContent = workspace.querySelector('[data-extension-workspace-direct-content]');
  let navigationVersion = 0;
  const base = window.location.href;
  const origin = window.location.origin;
  const appSubPath = workspace.dataset.appSubUrl;
  const generation = workspace.dataset.workspaceSessionGeneration;
  let invalidated = false;
  let pendingCheck;
  let checkVersion = 0;

  function invalidate() {
    if (invalidated) return;
    invalidated = true;
    navigationVersion++;
    if (navbar) navbar.hidden = true;
    if (footer) footer.hidden = true;
    if (frame) frame.src = 'about:blank';
    workspace.replaceChildren();
    workspace.style.display = '';
    workspace.textContent = 'Your session changed. Reload Forgejo to continue.';
    window.dispatchEvent(new Event('extension-workspace-invalidated'));
  }

  function checkSession() {
    if (invalidated || pendingCheck) return pendingCheck;
    workspace.style.display = 'none';
    if (navbar) navbar.hidden = true;
    if (footer) footer.hidden = true;
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
        presentDirect(workspace.hasAttribute('data-workspace-direct'));
      })
      .finally(() => {
        if (version === checkVersion) pendingCheck = null;
      });
    return pendingCheck;
  }

  if (!generation || !validAppSubURL(appSubPath)) {
    invalidate();
    return;
  }
  // A cached document stays hidden until the current native session is checked.
  const onPageShow = (event) => { if (event.persisted) checkSession(); };
  const onVisibilityChange = () => { if (document.visibilityState === 'visible') checkSession(); };
  window.addEventListener('pageshow', onPageShow);
  window.addEventListener('focus', checkSession);
  document.addEventListener('visibilitychange', onVisibilityChange);
  const initial = workspacePath(`${workspace.dataset.workspacePath}${window.location.hash}`, base, origin, appSubPath);
  if (!initial) {
    invalidate();
    return;
  }
  let current = initial;
  let activeDocument;
  let activeWindow;

  function workspacePage(path) {
    return workspaceDirectPath(new URL(path, origin).pathname);
  }

  function presentDirect(direct) {
    workspace.toggleAttribute('data-workspace-direct', direct);
    workspace.classList.toggle('extension-workspace-direct', direct);
    if (navbar) navbar.hidden = !direct;
    if (footer) footer.hidden = !direct;
    if (directContent) directContent.hidden = !direct;
    if (frame) frame.hidden = direct;
    workspace.dispatchEvent(new Event('extension-workspace-navigation'));
  }

  async function openWorkspace(path, push = false) {
    const version = ++navigationVersion;
    try {
      const response = await fetcher(new URL(path, origin).href, {
        credentials: 'same-origin', cache: 'no-store', redirect: 'error',
        headers: {Accept: 'text/html', 'X-Extension-Workspace-Navigation': '1'}, signal: AbortSignal.timeout(15000),
      });
      if (invalidated || version !== navigationVersion) return;
      if (!response.ok) { window.location.assign(path); return }
      if (response.headers.get('X-Extension-Session-Generation') !== generation) { invalidate(); return }
      const html = await response.text();
      if (invalidated || version !== navigationVersion) return;
      const native = new DOMParser().parseFromString(html, 'text/html');
      const incoming = native.querySelector('[data-workspace-direct]');
      const content = incoming?.querySelector('[data-extension-workspace-direct-content]');
      if (!content || incoming.dataset.workspaceSessionGeneration !== generation) { invalidate(); return }
      if (!directContent) {
        directContent = document.createElement('div');
        directContent.setAttribute('data-extension-workspace-direct-content', '');
        workspace.prepend(directContent);
      }
      // Only Fountain's native content slot is replaced. Persistent contribution
      // roots and terminal DOM never leave their owning document.
      const repositoryId = incoming.dataset.extensionRepositoryId ?? content.querySelector('[data-extension-repository-id]')?.dataset.extensionRepositoryId ?? '';
      directContent.replaceChildren(...content.childNodes);
      directContent.className = content.className;
      workspace.dataset.extensionRepositoryId = repositoryId;
      document.title = native.title;
      current = path;
      if (push) window.history.pushState(null, '', path);
      else updateHistory(path);
      if (frame) {
        activeDocument?.removeEventListener('click', onFrameClick);
        activeDocument?.removeEventListener('submit', onFrameSubmit);
        activeWindow?.removeEventListener('hashchange', onFrameNavigation);
        activeWindow?.removeEventListener('popstate', onFrameNavigation);
        activeDocument = activeWindow = null;
        frame.removeEventListener('load', onFrameLoad);
        frame.remove();
        frame = null;
      }
      presentDirect(true);
    } catch {
      if (!invalidated && version === navigationVersion) window.location.assign(path);
    }
  }

  function openNative(path, push = false, restoring = false) {
    navigationVersion++;
    if (!frame) {
      frame = document.createElement('iframe');
      frame.className = 'extension-workspace-frame';
      frame.title = 'Forgejo pages';
      frame.setAttribute('data-extension-workspace-frame', '');
      frame.setAttribute('sandbox', 'allow-same-origin allow-scripts allow-forms allow-downloads allow-modals allow-popups allow-top-navigation-by-user-activation');
      workspace.insertBefore(frame, workspace.querySelector('.extension-workspace-panels'));
      frame.addEventListener('load', onFrameLoad);
    }
    current = path;
    if (push) window.history.pushState(null, '', path);
    if (restoring && frame.contentWindow?.location.origin === origin) frame.contentWindow.location.replace(path);
    else frame.src = path;
    presentDirect(false);
  }

  function onNativeClick(event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = event.composedPath().find((node) => node?.localName === 'a' && 'href' in node);
    if (!link || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
    const decision = classifyWorkspaceNavigation(link.href, window.location.href, origin, appSubPath);
    if (decision.kind !== 'frame') return;
    event.preventDefault();
    if (workspacePage(decision.path)) openWorkspace(decision.path, true);
    else openNative(decision.path, true);
  }
  document.addEventListener('click', onNativeClick);

  function updateHistory(path) {
    const state = window.history.state;
    window.history.replaceState(state, '', path);
  }

  function onFrameClick(event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = event.composedPath().find((element) => element?.localName === 'a' && 'href' in element);
    if (!link || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
    const decision = classifyWorkspaceNavigation(link.href, frame.contentWindow.location.href, origin, appSubPath);
    if (decision.kind === 'frame') {
      if (!workspacePage(decision.path)) return;
      event.preventDefault();
      openWorkspace(decision.path, true);
      return;
    }
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
    if (workspacePage(decision.path)) { openWorkspace(decision.path); return }
    document.title = frame.contentDocument.title;
    if (decision.path !== current) {
      current = decision.path;
      updateHistory(current);
    }
  }

  function onFrameLoad() {
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
  }
  frame?.addEventListener('load', onFrameLoad);
  const onPopState = () => {
    if (invalidated) return;
    navigationVersion++;
    const path = workspacePath(`${window.location.pathname}${window.location.search}${window.location.hash}`, base, origin, appSubPath);
    if (!path || path === current) return;
    if (workspacePage(path)) openWorkspace(path);
    else openNative(path, false, true);
  };
  window.addEventListener('popstate', onPopState);
  const onPageHide = (event) => {
    navigationVersion++;
    checkVersion++;
    pendingCheck = null;
    workspace.style.display = 'none';
    if (event.persisted) return;
    frame?.removeEventListener('load', onFrameLoad);
    document.removeEventListener('click', onNativeClick);
    activeDocument?.removeEventListener('click', onFrameClick);
    activeDocument?.removeEventListener('submit', onFrameSubmit);
    activeWindow?.removeEventListener('hashchange', onFrameNavigation);
    activeWindow?.removeEventListener('popstate', onFrameNavigation);
    window.removeEventListener('pagehide', onPageHide);
    window.removeEventListener('pageshow', onPageShow);
    window.removeEventListener('focus', checkSession);
    window.removeEventListener('popstate', onPopState);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
  window.addEventListener('pagehide', onPageHide);
  updateHistory(initial);
  if (workspace.hasAttribute('data-workspace-direct')) presentDirect(true);
  else openNative(initial);
}
