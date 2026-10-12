import {retirePageFeatures, setPageConfig, initPageFeatures} from '../native-lifecycle.js';
import {submitEventSubmitter} from '../utils/dom.js';
import {mountContribution} from './extension-mount.js';

function isAuthPath(pathname) {
  return /(?:^|\/)user\/(?:login|logout|sign_up|forgot_password|forget_password|reset_password|recover_account|activate|activate_email|two_factor|webauthn|oauth2|openid|link_account|link_account_signin|link_account_signup)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)user\/settings\/(?:change_password|security|applications|keys)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)(?:install|login\/oauth|login\/openid|oauth2|openid)(?:\/|$)/.test(pathname);
}

function hasCredentialQuery(url) {
  return Array.from(url.searchParams.keys()).some((key) => /^(?:token|password|secret|client_secret|authorization|auth|code|credential|session|api_key|private_key|.*_token)$/i.test(key));
}

/** Classify a browser destination for host-handled native navigation. */
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
  return {kind: 'native', url: url.href, path: `${url.pathname}${url.search}${url.hash}`};
}

export function workspacePath(input, base, origin, appSubPath = '') {
  if (!input?.startsWith('/') || input.startsWith('//')) return null;
  const result = classifyWorkspaceNavigation(input, base, origin, appSubPath);
  return result.kind === 'native' ? result.path : null;
}

/** Report whether a pathname is a workspace root or an explicit global workspace alias. */
export function workspaceDirectPath(pathname, appSubPath = '') {
  if (pathname.endsWith('/workspace')) return true;
  if (!pathname.startsWith(appSubPath)) return false;
  const rest = pathname.slice(appSubPath.length);
  if (!rest.startsWith('/')) return false;
  if (rest === '/workspace/new') return true;
  const marker = '/workspace/requests/';
  if (!rest.startsWith(marker)) return false;
  return /^d[0-9a-f]{24}$/.test(rest.slice(marker.length));
}

const importExtension = (url) => import(/* webpackIgnore: true */ url);
const sessionCheckPath = (appSubPath) => `${appSubPath}/-/native/session`;

function validAppSubURL(value) {
  return typeof value === 'string' && (value === '' ||
    /^\/[^/\\?#\s]+(?:\/[^/\\?#\s]+)*$/.test(value) &&
    !/%2f|%5c/i.test(value) && !value.split('/').some((part) => part === '.' || part === '..'));
}

function readEnteredConfig(native) {
  const embedded = native.body?.dataset.pageData;
  if (typeof embedded !== 'string' || !embedded) return null;
  try {
    const parsed = JSON.parse(embedded);
    return parsed && typeof parsed === 'object' ? parsed : null;
  } catch {
    return null;
  }
}

function liveSessionMatches(url, generation, fetcher) {
  return Promise.resolve().then(() => fetcher(url, {credentials: 'same-origin', cache: 'no-store', redirect: 'manual'}))
    .then((response) => response.status === 204 && response.headers.get('X-Extension-Session-Generation') === generation)
    .catch(() => false);
}

export function initExtensionPages(importer = importExtension, fetcher = (...args) => window.fetch(...args)) {
  const roots = document.querySelectorAll('[data-extension-page], [data-extension-panel]');
  if (!roots.length) return;
  const pageRoots = new Set(Array.from(roots).filter((root) => root.hasAttribute('data-extension-page')));
  const mounted = [];
  const pending = new Map();
  let leaving = false;
  const settle = (owner) => {
    if (!owner) return;
    if (leaving) owner.dispose();
    else mounted.push(owner);
  };
  function mountRoot(root) {
    if (leaving || root.hasAttribute('data-extension-mounted')) return;
    root.setAttribute('data-extension-mounted', '');
    const controller = new AbortController();
    pending.set(controller, root);
    mountContribution(root, {importer, fetcher, signal: controller.signal}).then((owner) => {
      pending.delete(controller);
      settle(owner);
    });
  }
  for (const root of roots) mountRoot(root);
  function abortPending() {
    for (const controller of pending.keys()) controller.abort();
    pending.clear();
  }
  function disposeRoots() {
    if (leaving) return;
    leaving = true;
    abortPending();
    for (const owner of mounted.splice(0)) owner.dispose();
    window.removeEventListener('extension-workspace-invalidated', retireRoots);
    window.removeEventListener('extension-workspace-navigation', onWorkspaceNavigation);
  }
  function retireRoots() {
    if (leaving) return;
    leaving = true;
    abortPending();
    for (const owner of mounted.splice(0)) owner.retire('session-changed');
    window.removeEventListener('extension-workspace-invalidated', retireRoots);
    window.removeEventListener('extension-workspace-navigation', onWorkspaceNavigation);
  }
  // Each transplant swaps the page slot: page mounts follow their root
  // (dispose the detached owner, mount the fresh root) while retained panel
  // owners only receive the ordered presentation update.
  function syncContributionRoots(facts) {
    if (leaving) return;
    for (const [controller, root] of Array.from(pending)) {
      if (!root.isConnected || root.hasAttribute('data-extension-page')) {
        controller.abort();
        pending.delete(controller);
      }
    }
    const owned = new Set();
    for (const owner of Array.from(mounted)) {
      if (!owner.root.isConnected) {
        mounted.splice(mounted.indexOf(owner), 1);
        if (pageRoots.has(owner.root)) {
          pageRoots.delete(owner.root);
          owner.dispose();
        } else {
          owner.retire('contribution-unavailable');
        }
        continue;
      }
      owned.add(owner.root);
      owner.update({...facts, mode: pageRoots.has(owner.root) ? 'page' : 'panel', visible: true});
    }
    for (const root of document.querySelectorAll('[data-extension-page]')) {
      if (owned.has(root)) continue;
      pageRoots.add(root);
      mountRoot(root);
    }
  }
  function onWorkspaceNavigation(event) {
    syncContributionRoots(event.detail ?? {});
  }
  window.addEventListener('pagehide', (event) => { if (!event.persisted) disposeRoots(); });
  window.addEventListener('extension-workspace-invalidated', retireRoots);
  window.addEventListener('extension-workspace-navigation', onWorkspaceNavigation);
  if (!pageRoots.size) return;
  const firstPage = pageRoots.values().next().value;
  const generation = firstPage.dataset.extensionSessionGeneration;
  const appSubURL = firstPage.dataset.appSubUrl;
  const pageOwners = () => mounted.filter((owner) => pageRoots.has(owner.root));
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
    retireRoots();
  };
  const checkSession = () => {
    if (invalidated || pendingCheck) return;
    for (const root of pageRoots) root.style.display = 'none';
    const version = ++checkVersion;
    pendingCheck = liveSessionMatches(checkURL, generation, fetcher).then((matches) => {
      if (version !== checkVersion) return;
      if (!matches) invalidate();
      else {
        for (const root of pageRoots) root.style.display = '';
        for (const owner of pageOwners()) owner.resume({sessionGeneration: generation});
      }
    }).finally(() => { if (version === checkVersion) pendingCheck = null; });
  };
  const onPageShow = (event) => { if (event.persisted) checkSession(); };
  const onVisibilityChange = () => { if (document.visibilityState === 'visible') checkSession(); };
  const onPageHide = (event) => {
    checkVersion++;
    pendingCheck = null;
    for (const root of pageRoots) root.style.display = 'none';
    if (event.persisted) {
      for (const owner of pageOwners()) owner.suspend();
      return;
    }
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
  let directContent = workspace.querySelector('[data-extension-workspace-direct-content]');
  let navigationVersion = 0;
  let navigationController = null;
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
    navigationController?.abort();
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

  // H-0 transplantable layouts. Direct contribution documents carry their
  // content slot; /admin/users is the first native page whose feature
  // lifecycle is seam-managed (B02). Unknown layouts take a full document
  // load until their feature classes migrate.
  function transplantKind(path) {
    const pathname = new URL(path, origin).pathname;
    if (workspaceDirectPath(pathname, appSubPath)) return 'direct';
    if (pathname === `${appSubPath}/admin/users`) return 'plain';
    return null;
  }

  // Active link states are per-page chrome. States copy across matching
  // links so listeners on the live navbar survive the transition.
  function syncNavbarStates(native) {
    const live = document.getElementById('navbar');
    const incoming = native.getElementById('navbar');
    if (!live || !incoming) return;
    const incomingByHref = new Map();
    for (const link of incoming.querySelectorAll('a[href]')) {
      incomingByHref.set(new URL(link.getAttribute('href'), origin).href, link);
    }
    for (const link of live.querySelectorAll('a[href]')) {
      const peer = incomingByHref.get(new URL(link.getAttribute('href'), origin).href);
      if (!peer) continue;
      link.classList.toggle('active', peer.classList.contains('active'));
      if (peer.hasAttribute('aria-current')) link.setAttribute('aria-current', peer.getAttribute('aria-current'));
      else link.removeAttribute('aria-current');
    }
  }

  async function transplant(path, push = false) {
    const kind = transplantKind(path);
    if (!kind) {
      window.location.assign(path);
      return;
    }
    navigationController?.abort();
    navigationController = new AbortController();
    const version = ++navigationVersion;
    const signal = navigationController.signal;
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      navigationController.abort();
    }, 15000);
    let response;
    try {
      response = await fetcher(new URL(path, origin).href, {
        credentials: 'same-origin', cache: 'no-store', redirect: 'error',
        headers: {Accept: 'text/html', 'X-Extension-Workspace-Navigation': '1'}, signal,
      });
    } catch {
      clearTimeout(timer);
      if (!invalidated && version === navigationVersion && (timedOut || !signal.aborted)) window.location.assign(path);
      return;
    }
    clearTimeout(timer);
    if (invalidated || version !== navigationVersion || signal.aborted) return;
    if (!response.ok) {
      window.location.assign(path);
      return;
    }
    // P1: verify the entered document belongs to the live session before
    // touching any retained state. A foreign generation means the cookie or
    // actor changed out from under the shell: retire, never join.
    if (response.headers.get('X-Extension-Session-Generation') !== generation) {
      invalidate();
      return;
    }
    let html;
    try {
      html = await response.text();
    } catch {
      if (!invalidated && version === navigationVersion) window.location.assign(path);
      return;
    }
    if (invalidated || version !== navigationVersion || signal.aborted) return;
    const native = new DOMParser().parseFromString(html, 'text/html');
    const incoming = kind === 'direct' ? native.querySelector('[data-workspace-direct]') : native.body;
    const content = kind === 'direct' ?
      incoming?.querySelector('[data-extension-workspace-direct-content]') :
      native.querySelector('.page-content');
    const pageData = readEnteredConfig(native);
    if (!content || !pageData) {
      window.location.assign(path);
      return;
    }
    if (kind === 'direct' && incoming.dataset.workspaceSessionGeneration !== generation) {
      invalidate();
      return;
    }
    if (invalidated || version !== navigationVersion || signal.aborted) return;
    if (!directContent) {
      directContent = document.createElement('div');
      directContent.setAttribute('data-extension-workspace-direct-content', '');
      workspace.prepend(directContent);
    }
    // Only Fountain's native content slot is replaced. Persistent contribution
    // roots and terminal DOM never leave their owning document.
    const repositoryId = kind === 'direct' ?
      (incoming.dataset.extensionRepositoryId ?? content.querySelector('[data-extension-repository-id]')?.dataset.extensionRepositoryId ?? '') :
      '';
    try {
      retirePageFeatures();
      directContent.replaceChildren(...content.childNodes);
      directContent.className = content.className;
      syncNavbarStates(native);
      setPageConfig(pageData);
      initPageFeatures();
    } catch {
      window.location.assign(path);
      return;
    }
    workspace.dataset.extensionRepositoryId = repositoryId;
    workspace.dataset.workspacePath = path;
    document.title = native.title;
    current = path;
    if (push) window.history.pushState(null, '', path);
    else updateHistory(path);
    const target = new URL(path, origin);
    workspace.dispatchEvent(new CustomEvent('extension-workspace-navigation', {bubbles: true, detail: {
      route: `${target.pathname}${target.search}${target.hash}`,
      params: Object.fromEntries(target.searchParams),
      ...(repositoryId ? {repositoryId} : {}),
    }}));
  }

  function onWorkspaceClick(event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = event.composedPath().find((node) => node?.localName === 'a' && 'href' in node);
    if (!link || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
    const decision = classifyWorkspaceNavigation(link.href, window.location.href, origin, appSubPath);
    if (decision.kind === 'reject') {
      event.preventDefault();
      return;
    }
    if (decision.kind !== 'native') return;
    event.preventDefault();
    transplant(decision.path, true);
  }
  document.addEventListener('click', onWorkspaceClick);

  // GET forms navigate like links: same-origin GET submits to transplantable
  // paths stay in-shell. formFetchAction runs first (index.js order) and
  // claims .form-fetch-action forms; POST and targeted forms keep native load.
  function onWorkspaceSubmit(event) {
    if (event.defaultPrevented) return;
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.target && form.target !== '_self') return;
    if ((form.getAttribute('method') || 'get').toLowerCase() !== 'get') return;
    const formData = new FormData(form);
    const submitter = submitEventSubmitter(event);
    const submitterName = submitter?.getAttribute('name');
    if (submitterName) formData.append(submitterName, submitter.getAttribute('value') || '');
    const params = new URLSearchParams();
    for (const [key, value] of formData) params.append(key, value.toString());
    const target = new URL(form.getAttribute('action') || window.location.href, window.location.href);
    target.search = params.toString();
    const decision = classifyWorkspaceNavigation(target.href, window.location.href, origin, appSubPath);
    if (decision.kind !== 'native') return;
    event.preventDefault();
    transplant(decision.path, true);
  }
  document.addEventListener('submit', onWorkspaceSubmit);

  function updateHistory(path) {
    const state = window.history.state;
    window.history.replaceState(state, '', path);
  }

  const onPopState = () => {
    if (invalidated) return;
    navigationVersion++;
    const path = workspacePath(`${window.location.pathname}${window.location.search}${window.location.hash}`, base, origin, appSubPath);
    if (!path || path === current) return;
    transplant(path);
  };
  window.addEventListener('popstate', onPopState);
  const onPageHide = (event) => {
    navigationVersion++;
    navigationController?.abort();
    checkVersion++;
    pendingCheck = null;
    workspace.style.display = 'none';
    if (event.persisted) return;
    document.removeEventListener('click', onWorkspaceClick);
    document.removeEventListener('submit', onWorkspaceSubmit);
    window.removeEventListener('pagehide', onPageHide);
    window.removeEventListener('pageshow', onPageShow);
    window.removeEventListener('focus', checkSession);
    window.removeEventListener('popstate', onPopState);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
  window.addEventListener('pagehide', onPageHide);
  updateHistory(initial);
}
