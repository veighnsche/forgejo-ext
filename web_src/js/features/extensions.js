function isAuthPath(pathname) {
  return /(?:^|\/)user\/(?:login|logout|sign_up|forgot_password|forget_password|reset_password|recover_account|activate|activate_email|two_factor|webauthn|oauth2|openid|link_account|link_account_signin|link_account_signup)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)user\/settings\/change_password(?:\/|$)/.test(pathname) ||
    /(?:^|\/)(?:install|login\/oauth)(?:\/|$)/.test(pathname);
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

export async function mountExtension(root, importer = importExtension) {
  const {extensionEntry, extensionId, extensionPageId, extensionPanelId, extensionApiBase, extensionAssetBase} = root.dataset;
  const entry = classifyWorkspaceNavigation(extensionEntry, window.location.href, window.location.origin);
  const assets = classifyWorkspaceNavigation(extensionAssetBase, window.location.href, window.location.origin);
  if (entry.kind !== 'frame' || assets.kind !== 'frame' || !entry.url.startsWith(assets.url)) {
    root.textContent = 'Extension asset URL is invalid.';
    return () => {};
  }
  try {
    const module = await importer(entry.url);
    if (typeof module.mount !== 'function') throw new TypeError('Extension entry must export mount');
    const result = await module.mount(root, {
      extensionId,
      pageId: extensionPageId,
      panelId: extensionPanelId,
      apiBase: extensionApiBase,
      assetBase: extensionAssetBase,
    });
    return typeof result === 'function' ? result : typeof result?.dispose === 'function' ? () => result.dispose() : noop;
  } catch (error) {
    console.error('Could not load extension', error);
    root.textContent = 'Could not load this extension.';
    return noop;
  }
}

export function initExtensionPages(importer = importExtension) {
  const roots = document.querySelectorAll('[data-extension-page], [data-extension-panel]');
  if (!roots.length) return;
  const disposers = [];
  let leaving = false;
  for (const root of roots) mountExtension(root, importer).then((dispose) => {
    if (leaving) dispose();
    else disposers.push(dispose);
  });
  const onPageHide = (event) => {
    if (event.persisted) return;
    leaving = true;
    for (const dispose of disposers) dispose();
    window.removeEventListener('pagehide', onPageHide);
  };
  window.addEventListener('pagehide', onPageHide);
}

export function initExtensionWorkspace() {
  const workspace = document.querySelector('[data-extension-workspace]');
  if (!workspace) return;
  const frame = workspace.querySelector('[data-extension-workspace-frame]');
  const location = workspace.querySelector('[data-extension-workspace-location]');
  const toggle = workspace.querySelector('[data-extension-workspace-toggle]');
  const base = window.location.href;
  const origin = window.location.origin;
  const appSubPath = window.location.pathname.replace(/\/-\/extensions\/workspace$/, '');
  const initial = workspacePath(workspace.dataset.workspacePath, base, origin, appSubPath) || `${appSubPath}/`;
  let current = initial;
  let activeDocument;

  toggle?.addEventListener('click', () => {
    const focused = workspace.classList.toggle('focus-view');
    toggle.setAttribute('aria-expanded', String(!focused));
    toggle.textContent = focused ? 'Show panels' : 'Focus browsing';
    if (focused) frame.focus();
  });

  function updateHistory(path) {
    const address = new URL(window.location.href);
    address.searchParams.set('path', path);
    window.history.replaceState({extensionWorkspacePath: path}, '', address);
  }

  function onFrameClick(event) {
    const link = event.composedPath().find((element) => element?.localName === 'a' && 'href' in element);
    if (!link || link.target === '_blank' || event.defaultPrevented) return;
    const decision = classifyWorkspaceNavigation(link.href, frame.contentWindow.location.href, origin, appSubPath);
    if (decision.kind === 'frame') return;
    event.preventDefault();
    if (decision.kind === 'top') window.location.assign(decision.url);
  }

  frame.addEventListener('load', () => {
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
    activeDocument?.removeEventListener('click', onFrameClick, true);
    activeDocument = frame.contentDocument;
    activeDocument.addEventListener('click', onFrameClick, true);
    location.textContent = decision.path;
    if (decision.path !== current) {
      current = decision.path;
      updateHistory(current);
    }
  });
  updateHistory(initial);
  frame.src = initial;
}
