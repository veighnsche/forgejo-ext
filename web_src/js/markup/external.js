import {nativeMarkupElements} from './common.js';

let externalListener;

export function retireExternal() {
  window.removeEventListener('message', externalListener);
  externalListener = undefined;
}

export function renderExternal() {
  retireExternal();
  const [giteaExternalRender] = nativeMarkupElements('iframe.external-render');
  if (!giteaExternalRender) return;

  giteaExternalRender.contentWindow.postMessage({requestOffsetHeight: true}, '*');

  const eventListener = (event) => {
    if (event.source !== giteaExternalRender.contentWindow) return;
    const height = Number(event.data?.frameHeight);
    if (!height) return;
    giteaExternalRender.height = height;
    giteaExternalRender.style.overflow = 'hidden';
    window.removeEventListener('message', eventListener);
  };
  externalListener = eventListener;
  window.addEventListener('message', eventListener);
}
