const samples = [
  'abcdefghijklmnopqrstuvwxyz',
  'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
  '0123456789',
  '!"#$%&\'()*+,-./:;<=>?@[\\]^_`{|}~',
];
const pangramSizes = [12, 18, 24, 36];
const defaultPangram = 'The quick brown fox jumps over the lazy dog';

function makeSample(text) {
  const line = document.createElement('p');
  line.className = 'font-viewer-sample';
  line.textContent = text;
  return line;
}

function makePangram(text, size) {
  const label = document.createElement('small');
  label.className = 'font-viewer-size';
  label.textContent = `${size}px`;

  const line = document.createElement('p');
  line.className = 'font-viewer-pangram';
  line.style.setProperty('--font-viewer-font-size', `${size}px`);
  line.textContent = text;

  return [label, line];
}

let instanceCount = 0;

window.customElements.define(
  'font-viewer',
  class extends HTMLElement {
    connectedCallback() {
      const fallbackLink = this.innerHTML; // eslint-disable-line wc/no-child-traversal-in-connectedcallback

      const src = this.getAttribute('src');
      const pangram = this.getAttribute('pangram') || defaultPangram;
      const family = `font-preview-${instanceCount++}`;

      new FontFace(family, `url("${src}")`).load().then((font) => {
        document.fonts.add(font);
        this.style.setProperty('--font-viewer-family', `"${family}"`);
        this.replaceChildren(
          ...samples.map((text) => makeSample(text)),
          ...pangramSizes.flatMap((size) => makePangram(pangram, size)),
        );
      }).catch(() => {
        this.innerHTML = fallbackLink;
      });
    }
  },
);
