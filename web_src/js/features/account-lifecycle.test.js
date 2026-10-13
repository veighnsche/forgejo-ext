import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {initUserAuth, initUserAuthOauth2} from './user-auth.js';
import {initUserAuthWebAuthn, initUserAuthWebAuthnRegister} from './user-auth-webauthn.js';
import {initCaptcha} from './captcha.js';
import {initInstall} from './install.js';
import {GET} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));
vi.mock('./common-global.js', () => ({checkAppUrl: vi.fn()}));
vi.mock('../utils.js', async (importOriginal) => {
  const mod = await importOriginal();
  return {...mod, isDarkTheme: () => false};
});

const index = readFileSync('web_src/js/index.js', 'utf8');

afterEach(() => {
  document.body.innerHTML = '';
  vi.clearAllMocks();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  delete window.grecaptcha;
  delete window.turnstile;
  delete window.hcaptcha;
});

test('auth/install/captcha keep document boot handling, not page-feature transplant', () => {
  for (const name of ['initUserAuth', 'initUserAuthOauth2', 'initUserAuthWebAuthn', 'initUserAuthWebAuthnRegister', 'initCaptcha', 'initInstall']) {
    expect(index).toContain(`${name}();`);
    expect(index).not.toContain(`registerPageFeature('${name}`);
  }
  expect(index).not.toContain(`registerPageFeature('user-auth`);
  expect(index).not.toContain(`registerPageFeature('install`);
  expect(index).not.toContain(`registerPageFeature('captcha`);
});

test('oauth2 login binds once per entry and recovers after timeout', () => {
  vi.useFakeTimers();
  document.body.innerHTML = `<div id="oauth2-login-navigator"><div id="oauth2-login-navigator-inner"><a class="oauth-login-link" href="#">login</a></div></div>`;
  const timeout = vi.spyOn(window, 'setTimeout');
  initUserAuthOauth2();
  initUserAuthOauth2();
  const outer = document.getElementById('oauth2-login-navigator');
  const inner = document.getElementById('oauth2-login-navigator-inner');
  document.querySelector('.oauth-login-link').click();
  expect(outer.classList.contains('is-loading')).toBe(true);
  expect(inner.classList.contains('tw-invisible')).toBe(true);
  expect(timeout).toHaveBeenCalledTimes(1);
  vi.advanceTimersByTime(5000);
  expect(outer.classList.contains('is-loading')).toBe(false);
  expect(inner.classList.contains('tw-invisible')).toBe(false);
});

test('signin page checks current app URL on boot', () => {
  window.config.appUrl = `${window.location.origin}/`;
  document.body.innerHTML = `<div class="user signin"></div>`;
  expect(() => initUserAuth()).not.toThrow();
  expect(() => initUserAuth()).not.toThrow();
  document.body.innerHTML = `<div class="other"></div>`;
  expect(() => initUserAuth()).not.toThrow();
});

test('webauthn prompt is a no-op without its root and reports unsupported browsers', async () => {
  document.body.innerHTML = `<div class="other"></div>`;
  await expect(initUserAuthWebAuthn()).resolves.toBeUndefined();
  expect(GET).not.toHaveBeenCalled();
  document.body.innerHTML = `<div class="user signin webauthn-prompt"></div><div id="webauthn-error" class="tw-hidden"><span id="webauthn-error-msg"></span><span data-webauthn-error-msg="browser">unsupported</span></div>`;
  vi.stubGlobal('isSecureContext', true);
  window.PublicKeyCredential = undefined;
  await initUserAuthWebAuthn();
  expect(GET).not.toHaveBeenCalled();
  expect(document.getElementById('webauthn-error-msg').textContent).toContain('unsupported');
  delete window.PublicKeyCredential;
});

test('webauthn register disables without support and binds once with support', async () => {
  document.body.innerHTML = `<button id="register-webauthn">register</button><input id="nickname" value="key"><div id="webauthn-error" class="tw-hidden"><span id="webauthn-error-msg"></span></div>`;
  window.PublicKeyCredential = undefined;
  initUserAuthWebAuthnRegister();
  expect(document.getElementById('register-webauthn').disabled).toBe(true);
  delete window.PublicKeyCredential;
});

test('captcha renders each provider once with current theme and ignores unknown types', async () => {
  document.body.innerHTML = `<div id="captcha" data-captcha-type="g-recaptcha" data-sitekey="key"></div>`;
  const render = vi.fn();
  window.grecaptcha = {ready: (fn) => fn(), render};
  await initCaptcha();
  await initCaptcha();
  expect(render).toHaveBeenCalledTimes(1);
  document.body.innerHTML = `<div id="captcha" data-captcha-type="cf-turnstile" data-sitekey="key"></div>`;
  window.turnstile = {render: vi.fn()};
  await initCaptcha();
  expect(window.turnstile.render).toHaveBeenCalledTimes(1);
  document.body.innerHTML = `<div id="captcha" data-captcha-type="unknown" data-sitekey="key"></div>`;
  await expect(initCaptcha()).resolves.toBeUndefined();
  document.body.innerHTML = `<div class="other"></div>`;
  await expect(initCaptcha()).resolves.toBeUndefined();
});

test('install pre-boot wires database defaults once and post-install polls to login', async () => {
  document.body.innerHTML = `<div class="page-content install"><select id="db_type"><option value="sqlite3">sqlite</option><option value="postgres">pg</option></select>
    <div data-db-setting-for="postgres" class="tw-hidden"></div><div data-db-setting-for="common-host" class="tw-hidden"></div>
    <input id="db_host" value=""><input id="db_user" value=""><input id="db_name" value="">
    <input id="app_url" value="http://localhost:3000/"><input id="domain" value="localhost">
    <div id="offline-mode"><input type="checkbox"></div><div id="disable-gravatar"><input type="checkbox"></div>
    <div id="federated-avatar-lookup"><input type="checkbox"></div><div id="enable-openid-signin"><input type="checkbox"></div>
    <div id="disable-registration"><input type="checkbox"></div><div id="enable-captcha"><input type="checkbox"></div>
    <div id="enable-openid-signup"><input type="checkbox"></div></div>`;
  initInstall();
  const dbType = document.getElementById('db_type');
  dbType.value = 'postgres';
  dbType.dispatchEvent(new Event('change'));
  expect(document.getElementById('db_host').value).toBe('127.0.0.1:5432');
  expect(document.getElementById('db_user').value).toBe('forgejo');
});
