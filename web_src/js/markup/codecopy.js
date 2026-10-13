import {svg} from '../svg.js';
import {nativeMarkupElements} from './common.js';

export function makeCodeCopyButton() {
  const button = document.createElement('button');
  button.classList.add('code-copy', 'ui', 'button');
  button.innerHTML = svg('octicon-copy');
  return button;
}

export function renderCodeCopy() {
  const els = nativeMarkupElements('.markup .code-block code');
  if (!els.length) return;

  for (const el of els) {
    if (!el.textContent || el.nextElementSibling?.classList.contains('code-copy')) continue;
    const btn = makeCodeCopyButton();
    // remove final trailing newline introduced during HTML rendering
    btn.setAttribute('data-clipboard-text', el.textContent.replace(/\r?\n$/, ''));
    el.after(btn);
  }
}
