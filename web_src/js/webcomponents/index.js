import './relative-time.js';
import {OriginUrl} from './origin-url.js';
import './overflow-menu.js';
import './absolute-date.js';
import './lazy-webc.js'; // infrequently used components should be lazy-loaded with <lazy-webc tag="..." custom-attrs>...</>

window.customElements.define('origin-url', OriginUrl);
