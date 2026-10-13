import {clippie} from 'clippie';
import {showTemporaryTooltip} from '../modules/tippy.js';
import {convertImage} from '../utils.js';
import {GET} from '../modules/fetch.js';
import {contributionRootSelector} from '../native-lifecycle.js';

const {i18n} = window.config;
let copyController;

export function retireCopyContent() {
  copyController?.abort();
  copyController = undefined;
}

export function initCopyContent() {
  const btn = document.getElementById('copy-content');
  if (!btn || btn.classList.contains('disabled') || btn.closest(contributionRootSelector)) return;
  retireCopyContent();
  copyController = new AbortController();
  const signal = copyController.signal;

  btn.addEventListener('click', async () => {
    if (btn.classList.contains('is-loading')) return;
    let content;
    let isRasterImage = false;
    const link = btn.getAttribute('data-link');

    // when data-link is present, we perform a fetch. this is either because
    // the text to copy is not in the DOM or it is an image which should be
    // fetched to copy in full resolution
    if (link) {
      btn.classList.add('is-loading', 'loading-icon-2px');
      try {
        const res = await GET(link, {credentials: 'include', redirect: 'follow', signal});
        const contentType = res.headers.get('content-type');

        if (contentType.startsWith('image/') && !contentType.startsWith('image/svg')) {
          isRasterImage = true;
          content = await res.blob();
        } else {
          content = await res.text();
        }
      } catch {
        if (signal.aborted) return;
        return showTemporaryTooltip(btn, i18n.copy_error);
      } finally {
        btn.classList.remove('is-loading', 'loading-icon-2px');
      }
    } else { // text, read from DOM
      const lineEls = document.querySelectorAll('.file-view .lines-code');
      content = Array.from(lineEls, (el) => el.textContent).join('');
    }
    if (signal.aborted) return;

    // try copy original first, if that fails and it's an image, convert it to png
    const success = await clippie(content);
    if (signal.aborted) return;
    if (success) {
      showTemporaryTooltip(btn, i18n.copy_success);
    } else {
      if (isRasterImage) {
        const converted = await convertImage(content, 'image/png');
        if (signal.aborted) return;
        const success = await clippie(converted);
        if (signal.aborted) return;
        showTemporaryTooltip(btn, success ? i18n.copy_success : i18n.copy_error);
      } else {
        showTemporaryTooltip(btn, i18n.copy_error);
      }
    }
  }, {signal});
}
