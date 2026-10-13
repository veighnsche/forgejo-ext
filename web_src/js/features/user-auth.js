import {checkAppUrl} from './common-global.js';

const oauth2Bound = new Set();

export function initUserAuthOauth2() {
  const outer = document.getElementById('oauth2-login-navigator');
  if (!outer) return;
  const inner = document.getElementById('oauth2-login-navigator-inner');

  for (const el of oauth2Bound) {
    if (!el.isConnected) oauth2Bound.delete(el);
  }
  for (const link of outer.querySelectorAll('.oauth-login-link')) {
    if (oauth2Bound.has(link)) continue;
    oauth2Bound.add(link);
    link.addEventListener('click', () => {
      inner.classList.add('tw-invisible');
      outer.classList.add('is-loading');
      setTimeout(() => {
        // recover previous content to let user try again
        // usually redirection will be performed before this action
        outer.classList.remove('is-loading');
        inner.classList.remove('tw-invisible');
      }, 5000);
    });
  }
}

export function initUserAuth() {
  if (!document.querySelector('.user.signin')) return;
  checkAppUrl();
}
