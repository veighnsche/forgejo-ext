import {createTippy} from '../modules/tippy.js';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

const pickers = new Map();

export function retireColorPickers() {
  for (const {controller, tooltip, square} of pickers.values()) {
    controller.abort();
    tooltip.destroy();
    square.remove();
  }
  pickers.clear();
}

export async function initColorPickers() {
  const els = Array.from(document.getElementsByClassName('js-color-picker-input')).filter((el) => !el.closest(contributionRootSelector));
  if (!els.length) return;
  const generation = currentPageGeneration();

  await Promise.all([
    import(/* webpackChunkName: "colorpicker" */'vanilla-colorful/hex-color-picker.js'),
    import(/* webpackChunkName: "colorpicker" */'../../css/features/colorpicker.css'),
  ]);

  for (const el of els) {
    if (generation !== currentPageGeneration() || !el.isConnected || pickers.has(el)) continue;
    initPicker(el);
  }
}

function updateSquare(el, newValue) {
  el.style.color = /#[0-9a-f]{6}/i.test(newValue) ? newValue : 'transparent';
}

function updatePicker(el, newValue) {
  el.setAttribute('color', newValue);
}

function initPicker(el) {
  const controller = new AbortController();
  const {signal} = controller;
  const input = el.querySelector('input');

  const square = document.createElement('div');
  square.classList.add('preview-square');
  updateSquare(square, input.value);
  el.append(square);

  const picker = document.createElement('hex-color-picker');
  picker.addEventListener('color-changed', (e) => {
    input.value = e.detail.value;
    input.focus();
    updateSquare(square, e.detail.value);
  }, {signal});

  input.addEventListener('input', (e) => {
    updateSquare(square, e.target.value);
    updatePicker(picker, e.target.value);
  }, {signal});

  const tooltip = createTippy(input, {
    trigger: 'focus click',
    theme: 'bare',
    hideOnClick: true,
    content: picker,
    placement: 'bottom-start',
    interactive: true,
    onShow() {
      updatePicker(picker, input.value);
    },
  });
  pickers.set(el, {controller, tooltip, square});

  // init precolors
  for (const colorEl of el.querySelectorAll('.precolors .color')) {
    colorEl.addEventListener('click', (e) => {
      const newValue = e.target.getAttribute('data-color-hex');
      input.value = newValue;
      input.dispatchEvent(new Event('input', {bubbles: true}));
      updateSquare(square, newValue);
    }, {signal});
  }
}
