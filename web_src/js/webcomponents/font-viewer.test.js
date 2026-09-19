import './font-viewer.js';

// happy-dom does not support FontFace or document.fonts
function stubFontLoading({resolve}) {
  const add = vi.fn();
  Object.defineProperty(document, 'fonts', {value: {add}, configurable: true});

  vi.stubGlobal('FontFace', class {
    load() {
      return resolve ? Promise.resolve({}) : Promise.reject(new Error('load failed'));
    }
  });

  return {add};
}

test('font-viewer renders samples when the font loads', async () => {
  const {add} = stubFontLoading({resolve: true});

  const element = document.createElement('font-viewer');
  element.setAttribute('src', '/example.woff2');
  document.body.append(element);

  await vi.waitFor(() => expect(element.children).toHaveLength(12)); // 4 samples + 4 sizes * (label + pangram)
  expect(add).toHaveBeenCalledOnce();
  expect(element.style.getPropertyValue('--font-viewer-family')).toBeTruthy();
  expect(element.querySelector('.font-viewer-sample')).toBeTruthy();
  expect(element.querySelector('.font-viewer-size')).toBeTruthy();
  expect(element.querySelector('.font-viewer-pangram')).toBeTruthy();

  element.remove();
});

test('font-viewer keeps the fallback when the font fails to load', async () => {
  stubFontLoading({resolve: false});

  const element = document.createElement('font-viewer');
  element.setAttribute('src', '/missing.woff2');
  element.innerHTML = '<a href="/missing.woff2">download</a>';
  document.body.append(element);

  await vi.waitFor(() => expect(element.querySelector('a')).toBeTruthy());
  expect(element.querySelector('.font-viewer-sample')).toBeNull();

  element.remove();
});

test('font-viewer uses a custom pangram', async () => {
  stubFontLoading({resolve: true});

  const element = document.createElement('font-viewer');
  element.setAttribute('src', '/example.woff2');
  element.setAttribute('pangram', 'Sphinx of black quartz');
  document.body.append(element);

  await vi.waitFor(() => expect(element.querySelector('.font-viewer-pangram')).toBeTruthy());
  expect(element.querySelector('.font-viewer-pangram').textContent).toBe('Sphinx of black quartz');

  element.remove();
});
