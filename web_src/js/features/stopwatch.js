import {createTippy} from '../modules/tippy.js';

const {enableTimeTracking} = window.config;

export function initStopwatch() {
  if (!enableTimeTracking) {
    return;
  }

  const stopwatchEl = document.querySelector('.active-stopwatch-trigger');
  const stopwatchPopup = document.querySelector('.active-stopwatch-popup');

  if (!stopwatchEl || !stopwatchPopup) {
    return;
  }

  stopwatchEl.removeAttribute('href'); // intended for noscript mode only

  createTippy(stopwatchEl, {
    content: stopwatchPopup,
    placement: 'bottom-end',
    trigger: 'click',
    maxWidth: 'none',
    interactive: true,
    hideOnClick: true,
  });
}
