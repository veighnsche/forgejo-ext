import {isDarkTheme} from '../utils.js';

export async function initCaptcha() {
  const captchaEl = document.querySelector('#captcha');
  if (!captchaEl || captchaEl.dataset.captchaRendered === 'true') return;

  const siteKey = captchaEl.getAttribute('data-sitekey');
  const isDark = isDarkTheme();

  const params = {
    sitekey: siteKey,
    theme: isDark ? 'dark' : 'light',
  };

  switch (captchaEl.getAttribute('data-captcha-type')) {
    case 'g-recaptcha': {
      if (window.grecaptcha) {
        captchaEl.dataset.captchaRendered = 'true';
        window.grecaptcha.ready(() => {
          window.grecaptcha.render(captchaEl, params);
        });
      }
      break;
    }
    case 'cf-turnstile': {
      if (window.turnstile) {
        captchaEl.dataset.captchaRendered = 'true';
        window.turnstile.render(captchaEl, params);
      }
      break;
    }
    case 'h-captcha': {
      if (window.hcaptcha) {
        captchaEl.dataset.captchaRendered = 'true';
        window.hcaptcha.render(captchaEl, params);
      }
      break;
    }
    case 'm-captcha': {
      captchaEl.dataset.captchaRendered = 'true';
      const {default: mCaptcha} = await import(/* webpackChunkName: "mcaptcha-vanilla-glue" */'@mcaptcha/vanilla-glue');
      mCaptcha.INPUT_NAME = 'm-captcha-response';
      const instanceURL = captchaEl.getAttribute('data-instance-url');

      mCaptcha.default({
        siteKey: {
          instanceUrl: new URL(instanceURL),
          key: siteKey,
        },
      });
      break;
    }
    default:
  }
}
