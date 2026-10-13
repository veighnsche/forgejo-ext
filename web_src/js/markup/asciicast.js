import {nativeMarkupElements} from './common.js';
import {currentPageGeneration} from '../native-lifecycle.js';

const players = new Map();

export function retireAsciicast() {
  for (const player of players.values()) player.dispose();
  players.clear();
}

export async function renderAsciicast() {
  const els = nativeMarkupElements('.asciinema-player-container');
  if (!els.length) return;
  const generation = currentPageGeneration();

  const [player] = await Promise.all([
    import(/* webpackChunkName: "asciinema-player" */'asciinema-player'),
    import(/* webpackChunkName: "asciinema-player" */'asciinema-player/dist/bundle/asciinema-player.css'),
  ]);

  for (const el of els) {
    if (generation !== currentPageGeneration() || !el.isConnected || players.has(el)) continue;
    players.set(el, player.create(el.getAttribute('data-asciinema-player-src'), el, {
      // poster (a preview frame) to display until the playback is started.
      // Set it to 1 hour (also means the end if the video is shorter) to make the preview frame show more.
      poster: 'npt:1:0:0',
    }));
  }
}
